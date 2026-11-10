package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestQueryCtrlW(t *testing.T) {
	for _, tc := range []struct{ query, want string }{
		{"", ""}, {" ", ""}, {" \t　", ""},
		{"hello", ""}, {"hello world", "hello "}, {"hello world   ", "hello "},
		{"café déjà  ", "café "}, {"hello\tworld\t", "hello\t"},
		{"中文測試", ""}, {"中文 測試  ", "中文 "},
		{"日本語かなカナ", ""}, {"한국어 테스트　", "한국어 "},
		{"hello世界", "hello"}, {"世界hello", "世界"},
		{"hello 世界 test  ", "hello 世界 "}, {"hello test世界　", "hello test"},
		{"hello 👋🏽  ", "hello "},
	} {
		t.Run(tc.query, func(t *testing.T) {
			m := newModel(nil, tc.query, "all", nil, nil, false, 50, false)
			m.focus = true
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
			got := next.(model)
			if got.q != tc.want || !utf8.ValidString(got.q) {
				t.Fatalf("ctrl+w(%q) = %q, want %q", tc.query, got.q, tc.want)
			}
			if cmd == nil || got.revision != m.revision+1 {
				t.Error("query edit did not refresh results")
			}
		})
	}
	m := newModel(nil, "hello 世界test  ", "all", nil, nil, false, 50, false)
	m.focus = true
	for _, want := range []string{"hello 世界", "hello ", "", ""} {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
		m = next.(model)
		if m.q != want {
			t.Fatalf("repeated ctrl+w = %q, want %q", m.q, want)
		}
	}
}

func TestHelpCoversHandledKeys(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "ui.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	add := func(expr ast.Expr) {
		if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			key, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			keys[key] = true
		}
	}
	isKey := func(expr ast.Expr) bool {
		id, ok := expr.(*ast.Ident)
		return ok && id.Name == "key"
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Update" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.SwitchStmt:
				if isKey(n.Tag) {
					for _, stmt := range n.Body.List {
						for _, expr := range stmt.(*ast.CaseClause).List {
							add(expr)
						}
					}
				}
			case *ast.BinaryExpr:
				if n.Op == token.EQL {
					if isKey(n.X) {
						add(n.Y)
					}
					if isKey(n.Y) {
						add(n.X)
					}
				}
			}
			return true
		})
	}
	if len(keys) < 20 {
		t.Fatalf("failed to derive handled keys: %v", keys)
	}
	m := newModel(nil, "", "all", nil, nil, false, 50, false)
	m.width, m.height, m.help = 80, 24, true
	help := ansi.Strip(m.View())
	for key := range keys {
		label := key
		if key == "up" {
			label = "↑"
		}
		if key == "down" {
			label = "↓"
		}
		if !regexp.MustCompile(`(^|[^[:alnum:]+])` + regexp.QuoteMeta(label) + `($|[^[:alnum:]+])`).MatchString(help) {
			t.Errorf("help missing handled key %q:\n%s", key, help)
		}
	}
}

func TestHiddenHitListKeepsSelectedRowVisible(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys string
		sel  int
	}{
		{"full next", "vjv", 0},
		{"full last", "vjjjv", 0},
		{"full previous", "vkv", 3},
		{"help reload", "??", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := lookModel(false)
			m.width, m.height, m.sel = 80, 24, tc.sel
			m.rows = []hit{
				{UID: "a", Snippet: "first result"},
				{UID: "b", Snippet: "second result"},
				{UID: "c", Snippet: "third result"},
				{UID: "d", Snippet: "last result"},
			}
			for _, key := range tc.keys {
				next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
				m = next.(model)
				if m.help {
					next, _ = m.Update(resultMsg{revision: m.revision, rows: m.rows})
					m = next.(model)
				}
			}
			view := ansi.Strip(m.View())
			for _, line := range strings.Split(view, "\n") {
				if strings.Contains(line, m.rows[m.sel].Snippet) && strings.Contains(line, "▎") {
					return
				}
			}
			t.Fatalf("selected result is not visible after restoring the list:\n%s", view)
		})
	}
}

func TestHelpFooterAt80Columns(t *testing.T) {
	m := newModel(nil, "", "all", nil, nil, false, 50, false)
	for _, focus := range []bool{false, true} {
		m.width, m.height, m.focus = 80, 24, focus
		view := ansi.Strip(m.View())
		lines := strings.Split(view, "\n")
		footer := lines[len(lines)-1]
		pairs := []string{"t", "tools", "r", "sync", "?", "help"}
		if focus {
			pairs = []string{"ctrl+w", "word", "ctrl+u", "clear"}
		}
		for _, pair := range pairs {
			if !regexp.MustCompile(`(^|\s)` + regexp.QuoteMeta(pair) + `(\s|$)`).MatchString(footer) {
				t.Errorf("80-column footer missing %q: %q", pair, footer)
			}
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > 80 {
				t.Errorf("line exceeds 80 columns: %q", line)
			}
		}
	}
}
