package main

import "strings"

type harnessSpec struct {
	name, label, env, dir string
	mark, light, dark     string
	resume                []string
}

var harnesses = []harnessSpec{
	{"claude", "claude", "KIOKU_CLAUDE_DIR", ".claude/projects", "✻", "#8d5d1c", "#ddae6c", []string{"claude", "--resume", "{id}"}},
	{"codex", "codex", "KIOKU_CODEX_DIR", ".codex/sessions", "◇", "#147175", "#76c7cc", []string{"codex", "resume", "{id}"}},
	{"pi", "pi", "KIOKU_PI_DIR", ".pi/agent/sessions", "π", "#3b723e", "#8fc990", []string{"pi", "--session", "{path}"}},
	{"grok", "grok", "KIOKU_GROK_DIR", ".grok/sessions", "✕", "", "", []string{"cd", "{cwd}"}},
	{"opencode", "opencode", "KIOKU_OPENCODE_DB", ".local/share/opencode/opencode.db", "▢", "", "", []string{"opencode", "--session", "{id}"}},
	{"cursor", "cursor", "KIOKU_CURSOR_DIR", ".cursor/chats", "↖", "", "", []string{"cursor-agent", "--resume", "{id}"}},
	{"hermes", "hermes", "KIOKU_HERMES_DB", ".hermes/state.db", "h", "", "", []string{"hermes", "--resume", "{id}"}},
	{"agy", "agy", "KIOKU_AGY_DIR", ".gemini/antigravity-cli", "a", "", "", []string{"agy", "--conversation", "{id}"}},
}

func findHarness(name string) *harnessSpec {
	for i := range harnesses {
		if harnesses[i].name == name {
			return &harnesses[i]
		}
	}
	return nil
}

func harnessNames() []string {
	names := []string{"all"}
	for _, h := range harnesses {
		names = append(names, h.name)
	}
	return names
}

func harnessFlagHelp() string {
	names := harnessNames()
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

func harnessColors(dark bool) map[string]string {
	colors := map[string]string{}
	for _, h := range harnesses {
		color := h.light
		if dark {
			color = h.dark
		}
		if color != "" {
			colors[h.name] = color
		}
	}
	return colors
}
