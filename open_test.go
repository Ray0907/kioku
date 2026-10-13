package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTerminalEditor(t *testing.T) {
	for _, tt := range []struct {
		name string
		want bool
	}{
		{"vim", true}, {"nvim", true}, {"vi", true}, {"nano", true},
		{"hx", true}, {"helix", true}, {"micro", true}, {"kak", true},
		{"/opt/homebrew/bin/nvim", true}, {"/usr/bin/vim", true},
		{"zed", false}, {"cursor", false}, {"code", false}, {"subl", false},
		{"open", false}, {"xdg-open", false}, {"emacs", false},
		{"/usr/bin/emacs", false}, {"nvim-qt", false}, {"", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := terminalEditor(tt.name); got != tt.want {
				t.Fatalf("terminalEditor(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func fakeEditors(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

func TestEditorResolution(t *testing.T) {
	for _, tt := range []struct {
		name, override, visual, want string
		installed                    []string
	}{
		{"nvim only uses platform", "", "", "", []string{"nvim"}},
		{"vim only uses platform", "", "", "", []string{"vim"}},
		{"nvim and vim use platform", "", "", "", []string{"nvim", "vim"}},
		{"nvim with platform opener", "", "", "", []string{"nvim", "xdg-open"}},
		{"explicit nvim", "nvim", "", "nvim", []string{"zed", "nvim"}},
		{"zed before nvim", "", "", "zed", []string{"zed", "nvim"}},
		{"cursor before nvim", "", "", "cursor", []string{"cursor", "nvim"}},
		{"code before nvim", "", "", "code", []string{"code", "nvim"}},
		{"subl before nvim", "", "", "subl", []string{"subl", "nvim"}},
		{"GUI order", "", "", "zed", []string{"subl", "code", "cursor", "zed", "nvim", "vim"}},
		{"override wins", "vim", "nvim", "vim", []string{"zed", "cursor", "code", "subl", "nvim", "vim"}},
		{"visual wins", "", "vim -R", "vim -R", []string{"zed", "nvim", "vim"}},
		{"platform fallback", "", "", "", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := fakeEditors(t, tt.installed...)
			t.Setenv("KIOKU_EDITOR", tt.override)
			t.Setenv("VISUAL", tt.visual)
			t.Setenv("EDITOR", "nano") // EDITOR must not participate in resolution.
			wantLabel, wantArgs := tt.want, strings.Fields(tt.want)
			if tt.override == "" && tt.visual == "" {
				if tt.want == "" {
					wantLabel, wantArgs = platformEditor()
				} else {
					wantArgs = []string{filepath.Join(path, tt.want)}
				}
			}
			label, argv := editor()
			if label != wantLabel || !reflect.DeepEqual(argv, wantArgs) {
				t.Fatalf("editor() = %q, %v; want %q, %v", label, argv, wantLabel, wantArgs)
			}
		})
	}
}

func TestOpenProjectCommand(t *testing.T) {
	for _, tt := range []struct {
		name, override, visual string
		terminal, directory    bool
	}{
		{"nvim", "nvim", "", true, true},
		{"terminal args", "nvim -c 'set number'", "", true, true},
		{"VISUAL args", "", "vim -R", true, true},
		{"absolute vim", "/opt/homebrew/bin/vim -R", "", true, true},
		{"hx", "hx", "", true, true},
		{"helix", "helix", "", true, true},
		{"nano", "nano", "", true, false},
		{"micro", "micro -readonly", "", true, false},
		{"kak", "kak", "", true, false},
		{"vi", "vi", "", true, false},
		{"GUI", "zed", "", false, true},
		{"GUI args", "code --new-window", "", false, true},
		{"GUI emacs", "emacs", "", false, true},
		{"Finder", "open -a Zed", "", false, true},
		{"Linux opener", "xdg-open", "", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeEditors(t, "nvim", "vim", "hx", "helix", "nano", "micro", "kak", "vi", "zed", "code", "emacs", "open", "xdg-open")
			t.Setenv("KIOKU_EDITOR", tt.override)
			t.Setenv("VISUAL", tt.visual)
			setting := tt.override
			if setting == "" {
				setting = tt.visual
			}
			dir := t.TempDir()
			cmd, terminal, label := openProject(dir)
			if cmd == nil || terminal != tt.terminal || label != setting {
				t.Fatalf("openProject() = %v, %v, %q", cmd, terminal, label)
			}
			wantDir, target := "", dir
			if tt.terminal {
				wantDir, target = dir, "."
			}
			wantArgs := strings.Fields(setting)
			if tt.directory {
				wantArgs = append(wantArgs, target)
			}
			if cmd.Dir != wantDir || !reflect.DeepEqual(cmd.Args, wantArgs) {
				t.Fatalf("command Dir=%q Args=%v; want Dir=%q Args=%v", cmd.Dir, cmd.Args, wantDir, wantArgs)
			}
			if cmd.Process != nil || cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
				t.Fatal("command must remain unstarted with terminal streams unset")
			}
		})
	}
}

func TestOpenProjectMissingDirectory(t *testing.T) {
	t.Setenv("KIOKU_EDITOR", "nvim")
	dir := filepath.Join(t.TempDir(), "missing")
	cmd, terminal, status := openProject(dir)
	if cmd != nil || terminal || status != "directory no longer exists: "+dir {
		t.Fatalf("openProject() = %v, %v, %q", cmd, terminal, status)
	}
}

func TestOpenProjectNoEditor(t *testing.T) {
	t.Setenv("KIOKU_EDITOR", "   ")
	cmd, terminal, status := openProject(t.TempDir())
	if cmd != nil || terminal || status != "no editor configured" {
		t.Fatalf("openProject() = %v, %v, %q", cmd, terminal, status)
	}
}

func TestOpenStatus(t *testing.T) {
	status := openStatus("/project", "nvim", nil)
	if len(status) < 5 || !strings.HasSuffix(status, " opened /project in nvim") {
		t.Fatalf("unexpected success status: %q", status)
	}
	if _, err := time.Parse("15:04", status[:5]); err != nil {
		t.Fatalf("status timestamp: %v", err)
	}
	if got := openStatus("/project", "nvim", errors.New("exit status 1")); got != "exit status 1" {
		t.Fatalf("error status = %q", got)
	}
	updated, cmd := (model{}).Update(openMsg(status))
	if updated.(model).status != status || cmd != nil {
		t.Fatal("editor completion must update status without another command")
	}
}

func TestOpenKeyGUI(t *testing.T) {
	fakeEditors(t, "zed")
	t.Setenv("KIOKU_EDITOR", "zed")
	dir := t.TempDir()
	m := model{rows: []hit{{CWD: dir}}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd != nil || !strings.HasSuffix(updated.(model).status, " opened "+dir+" in zed") {
		t.Fatal("GUI open must start immediately and report success without a Tea command")
	}
	t.Setenv("KIOKU_EDITOR", "missing-editor")
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd != nil || !strings.Contains(updated.(model).status, "executable file not found") {
		t.Fatal("GUI launch failure must report the error")
	}
}

func TestOpenKeyTerminal(t *testing.T) {
	fakeEditors(t, "nvim")
	t.Setenv("KIOKU_EDITOR", "nvim")
	m := model{rows: []hit{{CWD: t.TempDir()}}, status: "before"}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if updated.(model).status != "before" || cmd == nil {
		t.Fatal("terminal open must return a command and defer status until editor exit")
	}
	if got := reflect.TypeOf(cmd()).String(); got != "tea.execMsg" {
		t.Fatalf("open command returns %s, want tea.ExecProcess message", got)
	}
}
