package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

const mcpProtocolVersion = "2025-06-18"

type mcpSchema struct {
	Type                 string                    `json:"type"`
	Properties           map[string]map[string]any `json:"properties"`
	Required             []string                  `json:"required"`
	AdditionalProperties bool                      `json:"additionalProperties"`
}

type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema mcpSchema       `json:"inputSchema"`
	Annotations map[string]bool `json:"annotations"`
}

func mcpTools() []mcpTool {
	tools := []mcpTool{
		{Name: "kioku_search", Description: "Search local agent messages. Empty query browses recent sessions; snippets are capped. Repeat the same arguments with next_cursor for more hits.", InputSchema: mcpSchema{Required: []string{"query"}, Properties: map[string]map[string]any{
			"query":        {"type": "string"},
			"harness":      {"type": "string", "enum": []string{"all", "claude", "codex", "pi", "grok", "opencode", "cursor"}},
			"project":      {"type": "string", "minLength": 1, "description": "Project basename, case-insensitive."},
			"limit":        {"type": "integer", "minimum": 1, "default": 10},
			"cursor":       {"type": "string"},
			"all_time":     {"type": "boolean", "default": false},
			"include_self": {"type": "boolean", "default": false},
		}}},
		{Name: "kioku_sessions", Description: "Find local agent sessions ranked by best hit, with compact snippets and topics. Empty query browses recent sessions.", InputSchema: mcpSchema{Required: []string{"query"}, Properties: map[string]map[string]any{
			"query":   {"type": "string"},
			"harness": {"type": "string", "enum": []string{"all", "claude", "codex", "pi", "grok", "opencode", "cursor"}},
			"project": {"type": "string", "minLength": 1, "description": "Project basename, case-insensitive."},
			"limit":   {"type": "integer", "minimum": 1, "default": 10},
			"cursor":  {"type": "string"},
		}}},
		{Name: "kioku_show", Description: "Read a message ref or session ID. Text is capped unless full=true (only the selected message); all=true pages the whole session, 10 messages at a time.", InputSchema: mcpSchema{Required: []string{"ref"}, Properties: map[string]map[string]any{
			"ref":     {"type": "string", "minLength": 1},
			"context": {"type": "integer", "minimum": 0, "default": 3},
			"query":   {"type": "string"},
			"full":    {"type": "boolean", "default": false},
			"all":     {"type": "boolean", "default": false},
			"cursor":  {"type": "string"},
		}}},
	}
	for i := range tools {
		tools[i].InputSchema.Type = "object"
		tools[i].Annotations = map[string]bool{"readOnlyHint": true}
	}
	return tools
}

type mcpArguments struct {
	Query       string `json:"query"`
	Harness     string `json:"harness"`
	Project     string `json:"project"`
	Limit       int    `json:"limit"`
	Cursor      string `json:"cursor"`
	Ref         string `json:"ref"`
	Context     int    `json:"context"`
	AllTime     bool   `json:"all_time"`
	IncludeSelf bool   `json:"include_self"`
	Full        bool   `json:"full"`
	All         bool   `json:"all"`
}

func mcpCall(params json.RawMessage) (string, error) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return "", fmt.Errorf("invalid tool call: %w", err)
	}
	var schema *mcpSchema
	for _, tool := range mcpTools() {
		if tool.Name == call.Name {
			schema = &tool.InputSchema
			break
		}
	}
	if schema == nil {
		return "", fmt.Errorf("unknown tool %q", call.Name)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &fields); err != nil || fields == nil {
		return "", fmt.Errorf("arguments must be an object")
	}
	for _, name := range schema.Required {
		if _, ok := fields[name]; !ok {
			return "", fmt.Errorf("missing argument %q", name)
		}
	}
	for name, value := range fields {
		if _, ok := schema.Properties[name]; !ok {
			return "", fmt.Errorf("unknown argument %q", name)
		}
		if bytes.Equal(value, []byte("null")) {
			return "", fmt.Errorf("argument %q cannot be null", name)
		}
	}
	a := mcpArguments{Harness: "all", Limit: 10, Context: 3}
	if err := json.Unmarshal(call.Arguments, &a); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if a.Limit < 1 {
		return "", fmt.Errorf("limit must be a positive integer")
	}
	if a.Context < 0 || a.Context == int(^uint(0)>>1) {
		return "", fmt.Errorf("context must be nonnegative and less than the maximum integer")
	}
	switch a.Harness {
	case "all", "claude", "codex", "pi", "grok", "opencode", "cursor":
	default:
		return "", fmt.Errorf("invalid harness %q", a.Harness)
	}
	var projects projectFlags
	if _, ok := fields["project"]; ok {
		if err := projects.Set(a.Project); err != nil {
			return "", err
		}
	}
	mode := "hits"
	if call.Name == "kioku_sessions" {
		mode = "sessions"
	}
	if call.Name == "kioku_show" {
		mode = "show"
		if a.Ref == "" {
			return "", fmt.Errorf("ref cannot be empty")
		}
	}
	key := pageKey{Mode: mode, Query: a.Query, Harness: a.Harness, Projects: projects, Limit: a.Limit, Context: a.Context, JSON: true, Ref: a.Ref, AllTime: a.AllTime && shortLatin(a.Query), IncludeSelf: a.IncludeSelf, Full: a.Full, All: a.All}
	offset, err := key.offset(a.Cursor)
	if err != nil {
		return "", err
	}
	// Sync the cache like --json, never rebuild or modify source transcripts.
	db, err := openDB()
	if err != nil {
		return "", err
	}
	defer db.Close()
	if _, err = syncIndex(db, false, nil); err != nil {
		return "", err
	}
	var page any
	ctx := context.Background()
	switch mode {
	case "sessions":
		page, err = compactSessions(ctx, db, key, offset)
	case "show":
		page, err = compactShow(ctx, db, key, offset)
	default:
		page, err = compactSearch(ctx, db, key, offset)
	}
	if err != nil {
		return "", err
	}
	var text bytes.Buffer
	enc := json.NewEncoder(&text)
	enc.SetEscapeHTML(false)
	if err = enc.Encode(page); err != nil {
		return "", err
	}
	return text.String(), nil
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

func mcpHandle(line []byte) *mcpResponse {
	response := &mcpResponse{JSONRPC: "2.0", ID: json.RawMessage("null")}
	fail := func(code int, message string) *mcpResponse {
		response.Error = &mcpError{code, message}
		return response
	}
	if !json.Valid(line) {
		return fail(-32700, "Parse error")
	}
	var request mcpRequest
	if err := json.Unmarshal(line, &request); err != nil || request.JSONRPC != "2.0" || request.Method == "" {
		// A usable ID still identifies an invalid request.
		if mcpValidID(request.ID) {
			response.ID = request.ID
		}
		return fail(-32600, "Invalid Request")
	}
	if request.ID != nil {
		if !mcpValidID(request.ID) {
			return fail(-32600, "Invalid request ID")
		}
		response.ID = request.ID
	}
	// Notifications (including notifications/initialized) never get a reply.
	if request.ID == nil || request.Method == "notifications/initialized" {
		return nil
	}
	if len(request.Params) > 0 && (request.Params[0] != '{' || !json.Valid(request.Params)) {
		return fail(-32602, "params must be an object")
	}
	switch request.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil || params.ProtocolVersion == "" {
			return fail(-32602, "protocolVersion must be a nonempty string")
		}
		response.Result = map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": "kioku", "version": version},
		}
	case "ping":
		response.Result = map[string]any{}
	case "tools/list":
		response.Result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		text, err := mcpCall(request.Params)
		result := map[string]any{}
		if err != nil {
			text = err.Error()
			result["isError"] = true
		}
		result["content"] = []map[string]string{{"type": "text", "text": text}}
		response.Result = result
	default:
		return fail(-32601, "Method not found")
	}
	return response
}

func mcpValidID(id json.RawMessage) bool {
	if len(id) == 0 {
		return false
	}
	// JSON-RPC IDs are strings or numbers; MCP excludes null IDs.
	return id[0] == '"' || id[0] == '-' || id[0] >= '0' && id[0] <= '9'
}

func readMCPLine(reader *bufio.Reader) (line []byte, tooLarge bool, err error) {
	const maxRequestSize = 1 << 20
	for {
		fragment, err := reader.ReadSlice('\n')
		fragment = bytes.TrimSuffix(fragment, []byte{'\n'})
		if !tooLarge {
			if len(fragment) > maxRequestSize-len(line) {
				line, tooLarge = nil, true
			} else {
				line = append(line, fragment...)
			}
		}
		// Drain oversized lines without retaining more bytes, including at EOF.
		if err != bufio.ErrBufferFull {
			return line, tooLarge, err
		}
	}
}

func serveMCP(input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	enc := json.NewEncoder(output)
	enc.SetEscapeHTML(false)
	for {
		line, tooLarge, err := readMCPLine(reader)
		var response *mcpResponse
		if tooLarge {
			response = &mcpResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &mcpError{-32600, "Request too large"}}
		} else if len(bytes.TrimSpace(line)) > 0 {
			response = mcpHandle(line)
		}
		if response != nil {
			if writeErr := enc.Encode(response); writeErr != nil {
				return writeErr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
