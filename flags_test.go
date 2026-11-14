package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFlagRegistrationParsesWithoutAnotherList(t *testing.T) {
	for _, after := range []bool{false, true} {
		opts := newFlagSet()
		text := opts.fs.String("extra-value", "", "test value")
		boolean := opts.fs.Bool("extra-bool", false, "test boolean")
		args := []string{"--extra-value", "--json", "--extra-bool"}
		if after {
			args = append([]string{"snapshot"}, args...)
		} else {
			args = append(args, "snapshot")
		}
		words, help, err := parseArgs(opts.fs, args)
		if err != nil || help || !reflect.DeepEqual(words, []string{"snapshot"}) || *text != "--json" || !*boolean {
			t.Fatalf("after=%v: words=%q help=%v error=%v value=%q bool=%v", after, words, help, err, *text, *boolean)
		}
	}
}

func TestEveryRegisteredFlag(t *testing.T) {
	newFlagSet().fs.VisitAll(func(f *flag.Flag) {
		boolean, ok := f.Value.(interface{ IsBoolFlag() bool })
		isBool := ok && boolean.IsBoolFlag()
		spelling, value := "--"+f.Name, f.DefValue
		if f.Name == "p" {
			spelling = "-p"
		}
		if f.Name == "p" || f.Name == "project" {
			value = "work/demo"
		}
		for _, equals := range []bool{false, true} {
			for _, after := range []bool{false, true} {
				t.Run(f.Name+"/"+map[bool]string{false: "separate", true: "equals"}[equals]+"/"+map[bool]string{false: "before", true: "after"}[after], func(t *testing.T) {
					opts := newFlagSet()
					args := []string{spelling}
					wantValue := value
					if isBool {
						wantValue = "true"
					}
					if equals {
						args[0] += "=" + wantValue
					} else if !isBool {
						args = append(args, value)
					}
					wantWords := []string{"snapshot"}
					if after {
						args = append([]string{"snapshot"}, args...)
					} else {
						args = append(args, "snapshot")
					}
					if equals && isBool {
						wantWords = args
						wantValue = f.DefValue
					}
					words, help, err := parseArgs(opts.fs, args)
					if err != nil || help || !reflect.DeepEqual(words, wantWords) {
						t.Fatalf("words=%q help=%v error=%v; want %q", words, help, err, wantWords)
					}
					if f.Name == "p" || f.Name == "project" {
						wantValue = filepath.Base(value)
					}
					if got := opts.fs.Lookup(f.Name).Value.String(); got != wantValue {
						t.Fatalf("value=%q, want %q", got, wantValue)
					}
					visited := false
					opts.fs.Visit(func(set *flag.Flag) { visited = visited || set.Name == f.Name })
					if visited == (equals && isBool) {
						t.Fatalf("explicit flag visit=%v", visited)
					}
				})
			}
		}
		if !isBool {
			t.Run(f.Name+"/missing", func(t *testing.T) {
				opts := newFlagSet()
				words, help, err := parseArgs(opts.fs, []string{spelling})
				if f.Name == "harness" {
					if err != nil || help || !reflect.DeepEqual(words, []string{spelling}) {
						t.Fatalf("trailing harness: words=%q help=%v error=%v", words, help, err)
					}
				} else if err == nil || err.Error() != spelling+" requires a value" {
					t.Fatalf("missing value: %v", err)
				}
			})
		}
	})
}

func TestFlagArgumentEdges(t *testing.T) {
	for _, tc := range []struct {
		name        string
		args, words []string
		help        bool
		err         string
		values      map[string]string
	}{
		{name: "flag-looking string", args: []string{"--query", "--json", "snapshot"}, words: []string{"snapshot"}, values: map[string]string{"query": "--json", "json": "false"}},
		{name: "negative number", args: []string{"--limit", "-1"}, values: map[string]string{"limit": "-1"}},
		{name: "flag-looking harness", args: []string{"--harness", "--json", "snapshot"}, words: []string{"snapshot"}, values: map[string]string{"harness": "--json", "json": "false"}},
		{name: "empty harness", args: []string{"--harness="}, values: map[string]string{"harness": ""}},
		{name: "projects in order", args: []string{"-p", "work/first", "--project", "work/second", "-p", "work/third"}, values: map[string]string{"project": "first,second,third", "p": "first,second,third"}},
		{name: "empty short project", args: []string{"-p", ""}, err: "project name cannot be empty"},
		{name: "empty long project", args: []string{"--project="}, err: "project name cannot be empty"},
		{name: "unknown spellings", args: []string{"--bogus", "-x", "-limit", "-json", "-rebuild", "--p", "demo"}, words: []string{"--bogus", "-x", "-limit", "-json", "-rebuild", "--p", "demo"}},
		{name: "terminator", args: []string{"snapshot", "--", "--help", "--json", "-p", "demo"}, words: []string{"snapshot", "--help", "--json", "-p", "demo"}},
		{name: "boolean equals", args: []string{"--json=true", "--json=false"}, words: []string{"--json=true", "--json=false"}, values: map[string]string{"json": "false"}},
		{name: "help first", args: []string{"-h", "snapshot"}, help: true},
		{name: "help after", args: []string{"snapshot", "--help", "--limit"}, words: []string{"snapshot"}, help: true},
		{name: "help as string value", args: []string{"--query", "--help"}, values: map[string]string{"query": "--help"}},
		{name: "help as numeric value", args: []string{"--limit", "--help"}, err: `invalid value "--help" for flag -limit: parse error`},
		{name: "last wins", args: []string{"--limit", "1", "--limit", "2"}, values: map[string]string{"limit": "2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := newFlagSet()
			var stderr bytes.Buffer
			opts.fs.SetOutput(&stderr)
			words, help, err := parseArgs(opts.fs, tc.args)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("error=%v, want %q", err, tc.err)
				}
				if !strings.Contains(stderr.String(), "Usage of kioku:\n") {
					t.Fatalf("missing flag package usage: %q", stderr.String())
				}
				return
			}
			if err != nil || help != tc.help || !reflect.DeepEqual(words, tc.words) {
				t.Fatalf("words=%q help=%v error=%v; want words=%q help=%v", words, help, err, tc.words, tc.help)
			}
			for name, want := range tc.values {
				if got := opts.fs.Lookup(name).Value.String(); got != want {
					t.Errorf("%s=%q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestFlagExplicitVisits(t *testing.T) {
	opts := newFlagSet()
	_, _, err := parseArgs(opts.fs, []string{"--limit", "1", "--since=", "--until=", "--max-per-session", "0", "--role=", "--model="})
	if err != nil {
		t.Fatal(err)
	}
	var visited []string
	opts.fs.Visit(func(f *flag.Flag) { visited = append(visited, f.Name) })
	if want := []string{"limit", "max-per-session", "model", "role", "since", "until"}; !reflect.DeepEqual(visited, want) {
		t.Fatalf("visited=%q, want %q", visited, want)
	}
	opts = newFlagSet()
	opts.fs.Visit(func(f *flag.Flag) { t.Errorf("default visited: %s", f.Name) })
}

// The same cases exercise run before and after the parser refactor.
func TestFlagBehavior(t *testing.T) {
	home := doctorHome(t)
	cwd := filepath.Join(home, "work", "demo")
	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	cwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldCWD); err != nil {
			t.Error(err)
		}
	})
	rows := []map[string]any{
		{"type": "user", "timestamp": "2026-09-27T10:00:00Z", "cwd": cwd, "sessionId": "flags-session", "message": map[string]any{"role": "user", "content": "snapshot question"}},
		{"type": "assistant", "timestamp": "2026-09-27T10:00:01Z", "cwd": cwd, "sessionId": "flags-session", "message": map[string]any{"role": "assistant", "model": "fixture-model", "content": "snapshot answer"}},
	}
	var data bytes.Buffer
	for _, row := range rows {
		if err := json.NewEncoder(&data).Encode(row); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(home, ".claude", "projects", "demo", "flags.jsonl")
	doctorFile(t, path)
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, value  string
		valued, show bool
	}{
		{"rebuild", "", false, false}, {"json", "", false, false},
		{"limit", "1", true, false}, {"cursor", "", true, false},
		{"query", "snapshot", true, true}, {"context", "0", true, true},
		{"here", "", false, false}, {"all-time", "", false, false},
		{"include-self", "", false, false}, {"include-automated", "", false, false},
		{"role", "user", true, false}, {"model", "fixture", true, false},
		{"all", "", false, true}, {"full", "", false, true},
		{"outline", "", false, true}, {"sessions", "", false, false},
		{"format", "text", true, true}, {"since", "2026-09-01", true, false},
		{"until", "2026-10-01", true, false}, {"facets", "", false, false},
		{"max-per-session", "1", true, false}, {"no-redact", "", false, false},
		{"no-mouse", "", false, false}, {"harness", "claude", true, false},
		{"project", "demo", true, false}, {"p", "demo", true, false},
		{"version", "", false, false},
	} {
		for _, after := range []bool{false, true} {
			position := "before"
			if after {
				position = "after"
			}
			t.Run(tc.name+"/"+position, func(t *testing.T) {
				flag := "--" + tc.name
				if tc.name == "p" {
					flag = "-p"
				}
				args := []string{flag}
				if tc.valued {
					args = append(args, tc.value)
				}
				word := "snapshot"
				if tc.show {
					word = "flags-session:0"
				}
				if after {
					args = append([]string{word}, args...)
				} else {
					args = append(args, word)
				}
				if tc.name != "json" {
					args = append(args, "--json")
				}
				if tc.show {
					args = append([]string{"show"}, args...)
				}
				stdout, stderr := warningRun(t, args...)
				if stderr != "" {
					t.Fatalf("stderr = %q", stderr)
				}
				if tc.name == "version" {
					if stdout != version+"\n" {
						t.Fatalf("version = %q", stdout)
					}
					return
				}
				var page struct {
					Total    int `json:"total"`
					Messages []struct {
						Text string `json:"text"`
					} `json:"messages"`
				}
				if err := json.Unmarshal([]byte(stdout), &page); err != nil {
					t.Fatal(err)
				}
				if tc.show {
					if len(page.Messages) == 0 || !strings.Contains(page.Messages[0].Text, "snapshot") {
						t.Fatalf("missing transcript: %s", stdout)
					}
				} else if page.Total == 0 {
					t.Fatalf("flag swallowed into query: %s", stdout)
				}
			})
		}
	}
}
