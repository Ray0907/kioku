package main

import "strings"

type harnessSpec struct {
	name, label, env, dir string
	mark                  string
	light, dark           agentColors
	resume                []string
}

type agentColors struct{ fg, chip string }

var harnesses = []harnessSpec{
	{"claude", "claude", "KIOKU_CLAUDE_DIR", ".claude/projects", "✻", agentColors{"#8d5d1c", "#f4efe8"}, agentColors{"#ddae6c", "#37312c"}, []string{"claude", "--resume", "{id}"}},
	{"codex", "codex", "KIOKU_CODEX_DIR", ".codex/sessions", "◇", agentColors{"#147175", "#e8f1f1"}, agentColors{"#76c7cc", "#253639"}, []string{"codex", "resume", "{id}"}},
	{"pi", "pi", "KIOKU_PI_DIR", ".pi/agent/sessions", "π", agentColors{"#3b723e", "#ebf1ec"}, agentColors{"#8fc990", "#27352b"}, []string{"pi", "--session", "{path}"}},
	{"grok", "grok", "KIOKU_GROK_DIR", ".grok/sessions", "✕", agentColors{"#7d5e2a", "#f5efe6"}, agentColors{"#d4b483", "#363026"}, []string{"cd", "{cwd}"}},
	{"opencode", "opencode", "KIOKU_OPENCODE_DB", ".local/share/opencode/opencode.db", "▢", agentColors{"#47648c", "#ebf0f7"}, agentColors{"#9bb3d6", "#2a313d"}, []string{"opencode", "--session", "{id}"}},
	{"cursor", "cursor", "KIOKU_CURSOR_DIR", ".cursor/chats", "↖", agentColors{"#1b6ca8", "#e7f1fa"}, agentColors{"#7dc4e4", "#25343d"}, []string{"cursor-agent", "--resume", "{id}"}},
	{"hermes", "hermes", "KIOKU_HERMES_DB", ".hermes/state.db", "❖", agentColors{"#9b3b72", "#f9eaf2"}, agentColors{"#e599c2", "#3b2835"}, []string{"hermes", "--resume", "{id}"}},
	{"agy", "agy", "KIOKU_AGY_DIR", ".gemini/antigravity-cli", "✦", agentColors{"#6b46c1", "#efeafc"}, agentColors{"#b89bf8", "#312b42"}, []string{"agy", "--conversation", "{id}"}},
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

func harnessColors(dark bool) map[string]agentColors {
	colors := make(map[string]agentColors, len(harnesses))
	for _, h := range harnesses {
		if dark {
			colors[h.name] = h.dark
		} else {
			colors[h.name] = h.light
		}
	}
	return colors
}
