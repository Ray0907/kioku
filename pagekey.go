package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type pageKey struct {
	Mode, Query, Harness, Ref                  string
	Projects                                   []string
	Roles                                      []string `json:",omitempty"`
	Model                                      string   `json:",omitempty"`
	Limit, Context                             int
	JSON, AllTime, All, Full, NoMouse, Rebuild bool
	Outline                                    bool   `json:",omitempty"`
	IncludeSelf                                bool   `json:",omitempty"`
	IncludeAutomated                           bool   `json:",omitempty"`
	Since, Until                               string `json:",omitempty"`
	Root                                       string `json:",omitempty"`
	Facets                                     bool   `json:",omitempty"`
	MaxPerSession                              int    `json:",omitempty"`
}

func (k pageKey) rng() timeRange { return timeRange{k.Since, k.Until} }

func (k pageKey) fingerprint() string {
	b, _ := json.Marshal(k)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}
func (k pageKey) cursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("1:" + k.fingerprint() + ":" + strconv.Itoa(offset)))
}
func (k pageKey) offset(token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	if len(token) > 128 {
		return 0, fmt.Errorf("invalid cursor")
	}
	b, e := base64.RawURLEncoding.DecodeString(token)
	if e != nil {
		return 0, fmt.Errorf("invalid cursor")
	}
	parts := strings.Split(string(b), ":")
	if len(parts) != 3 || parts[0] != "1" {
		return 0, fmt.Errorf("invalid cursor")
	}
	if parts[1] != k.fingerprint() {
		return 0, fmt.Errorf("cursor does not match this query or flags")
	}
	n, e := strconv.Atoi(parts[2])
	if e != nil || n < 1 || n > int(^uint(0)>>1)-k.Limit {
		return 0, fmt.Errorf("invalid cursor offset")
	}
	return n, nil
}
func nextCursor(k pageKey, offset, shown, total int) string {
	if offset+shown < total && shown > 0 {
		return k.cursor(offset + shown)
	}
	return ""
}
