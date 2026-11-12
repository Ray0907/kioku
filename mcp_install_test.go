package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Exercise main's dispatch and exit status in a process with only fake CLIs and a private HOME.
func TestMCPInstallProcess(t *testing.T) {
	if os.Getenv("KIOKU_INSTALL_TEST_CHILD") != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("KIOKU_INSTALL_TEST_ARGS")), &args); err != nil {
		panic(err)
	}
	os.Args = append([]string{"kioku"}, args...)
	main()
	os.Exit(0)
}

type installFixture struct{ bin, home, log, exe, kioku string }

func installSetup(t *testing.T, agents ...string) installFixture {
	t.Helper()
	dir := t.TempDir()
	f := installFixture{bin: filepath.Join(dir, "bin with spaces"), home: filepath.Join(dir, "home"), log: filepath.Join(dir, "argv")}
	var err error
	f.exe, err = os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{f.bin, f.home} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	f.kioku = filepath.Join(f.bin, "kioku")
	if err := os.Symlink(f.exe, f.kioku); err != nil {
		t.Fatal(err)
	}
	for _, agent := range agents {
		f.agent(t, agent, "")
	}
	return f
}

func (f installFixture) agent(t *testing.T, name, extra string) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\000' '%s' "$@" >> "$INSTALL_LOG"
printf '\n' >> "$INSTALL_LOG"
%s
if [ "$2" = remove ]; then
 if [ ! -f "$HOME/%s-entry" ]; then
  printf 'No MCP server named "kioku" in user scope\n' >&2
  exit 1
 fi
 /bin/rm "$HOME/%s-entry"
elif [ "$2" = add ]; then
 if [ '%s' = claude ] && [ -f "$HOME/%s-entry" ]; then
  printf 'already exists\n' >&2
  exit 1
 fi
 : > "$HOME/%s-entry"
 printf 'child stdout\n'
 printf 'child stderr\n' >&2
fi
`, name, extra, name, name, name, name, name)
	if err := os.WriteFile(filepath.Join(f.bin, name), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
}

func (f installFixture) run(t *testing.T, input string, args ...string) (string, string, error) {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(f.exe, "-test.run=^TestMCPInstallProcess$")
	cmd.Env = []string{"PATH=" + f.bin, "HOME=" + f.home, "INSTALL_LOG=" + f.log, "KIOKU_INSTALL_TEST_CHILD=1", "KIOKU_INSTALL_TEST_ARGS=" + string(encoded), "KIOKU_INDEX=" + filepath.Join(f.home, "index.db")}
	cmd.Stdin = strings.NewReader(input)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err = cmd.Run()
	return out.String(), stderr.String(), err
}

func (f installFixture) calls(t *testing.T) [][]string {
	t.Helper()
	b, err := os.ReadFile(f.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		calls = append(calls, strings.Split(strings.TrimSuffix(line, "\x00"), "\x00"))
	}
	return calls
}

func TestMCPInstallExactArgv(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "pi", "grok"} {
		t.Run(agent, func(t *testing.T) {
			f := installSetup(t, agent)
			out, stderr, err := f.run(t, "", "mcp", "install", agent, "--yes")
			if err != nil {
				t.Fatalf("%v: %s%s", err, out, stderr)
			}
			add := []string{agent, "mcp", "add", "kioku", "--", f.kioku, "mcp"}
			var want [][]string
			if agent == "claude" {
				want = append(want, []string{agent, "mcp", "remove", "--scope", "user", "kioku"})
				add = []string{agent, "mcp", "add", "--scope", "user", "kioku", "--", f.kioku, "mcp"}
			}
			if agent == "grok" {
				want = append(want, []string{agent, "mcp", "remove", "kioku"})
			}
			want = append(want, add)
			if got := f.calls(t); !reflect.DeepEqual(got, want) {
				t.Fatalf("calls=%q want=%q", got, want)
			}
			if !filepath.IsAbs(f.kioku) || !strings.Contains(out, "restart") || !strings.Contains(out, agent+": OK") || !strings.Contains(out, "child stdout") || !strings.Contains(stderr, "child stderr") {
				t.Fatalf("out=%q stderr=%q", out, stderr)
			}
		})
	}
}

func TestMCPInstallDetection(t *testing.T) {
	f := installSetup(t, "codex", "pi")
	out, stderr, err := f.run(t, "", "mcp", "install", "--yes")
	if err != nil {
		t.Fatalf("%v: %s%s", err, out, stderr)
	}
	want := [][]string{{"codex", "mcp", "add", "kioku", "--", f.kioku, "mcp"}, {"pi", "mcp", "add", "kioku", "--", f.kioku, "mcp"}}
	if got := f.calls(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls=%q", got)
	}
	if !strings.Contains(out, "Agents: codex, pi") {
		t.Fatalf("out=%q", out)
	}
}

func TestMCPInstallNoConsent(t *testing.T) {
	for _, args := range [][]string{{"--dry-run"}, {"codex", "--yes", "--dry-run"}, {}, {"codex"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			f := installSetup(t, "codex")
			out, stderr, err := f.run(t, "y\n", append([]string{"mcp", "install"}, args...)...)
			dry := strings.Contains(strings.Join(args, " "), "--dry-run")
			if (err == nil) != dry {
				t.Fatalf("err=%v out=%q stderr=%q", err, out, stderr)
			}
			if got := f.calls(t); got != nil {
				t.Fatalf("ran without consent: %q", got)
			}
			if !strings.Contains(out, "codex mcp add kioku --") || !strings.Contains(out, fmt.Sprintf("%q", f.kioku)) {
				t.Fatalf("missing quoted command: %q", out)
			}
		})
	}
}

func TestMCPInstallInvalidNames(t *testing.T) {
	for _, name := range []string{"unknown", "; rm", "--evil"} {
		t.Run(name, func(t *testing.T) {
			f := installSetup(t, "codex")
			_, stderr, err := f.run(t, "", "mcp", "install", "codex", name, "--yes")
			if err == nil {
				t.Fatal("accepted unknown name")
			}
			for _, known := range []string{"claude", "codex", "pi", "grok"} {
				if !strings.Contains(stderr, known) {
					t.Fatalf("missing known name: %q", stderr)
				}
			}
			if got := f.calls(t); got != nil {
				t.Fatalf("ran invalid input: %q", got)
			}
		})
	}
}

func TestMCPInstallContinuesAfterFailure(t *testing.T) {
	f := installSetup(t, "codex", "pi")
	f.agent(t, "codex", "exit 7")
	out, stderr, err := f.run(t, "", "mcp", "install", "codex", "pi", "--yes")
	if err == nil || len(f.calls(t)) != 2 || !strings.Contains(out, "codex: FAILED") || !strings.Contains(out, "pi: OK") || strings.Contains(out, "restart") {
		t.Fatalf("err=%v out=%q stderr=%q calls=%q", err, out, stderr, f.calls(t))
	}
}

func TestMCPInstallMissingAgents(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(fmt.Sprint(named), func(t *testing.T) {
			f := installSetup(t)
			args := []string{"mcp", "install", "--yes"}
			if named {
				f.agent(t, "pi", "")
				args = append(args, "codex", "pi")
			}
			out, stderr, err := f.run(t, "", args...)
			if err == nil {
				t.Fatal("missing agent succeeded")
			}
			if named {
				if !strings.Contains(out, "codex: FAILED") || !strings.Contains(out, "pi: OK") {
					t.Fatalf("out=%q", out)
				}
			} else if !strings.Contains(stderr, "no known agents") {
				t.Fatalf("stderr=%q", stderr)
			}
		})
	}
}

func TestMCPInstallRepeat(t *testing.T) {
	f := installSetup(t, "claude", "codex", "pi", "grok")
	for i := 0; i < 2; i++ {
		out, stderr, err := f.run(t, "", "mcp", "install", "--yes")
		if err != nil {
			t.Fatalf("run %d: %v %s%s", i, err, out, stderr)
		}
		if i == 0 {
			oldBin := f.bin
			f.bin += " updated"
			if err := os.Rename(oldBin, f.bin); err != nil {
				t.Fatal(err)
			}
			f.kioku = filepath.Join(f.bin, "kioku")
		}
	}
	for _, call := range f.calls(t)[6:] {
		if call[2] == "add" && call[len(call)-2] != f.kioku {
			t.Fatalf("stale path: %q", call)
		}
	}
	if got := len(f.calls(t)); got != 12 {
		t.Fatalf("got %d calls", got)
	}
}

func TestMCPInstallRemoveError(t *testing.T) {
	f := installSetup(t, "claude", "pi")
	f.agent(t, "claude", "if [ \"$2\" = remove ]; then printf 'permission denied\\n' >&2; exit 1; fi")
	out, stderr, err := f.run(t, "", "mcp", "install", "claude", "pi", "--yes")
	if err == nil || len(f.calls(t)) != 2 || !strings.Contains(out, "claude: FAILED") || !strings.Contains(out, "pi: OK") || !strings.Contains(stderr, "permission denied") {
		t.Fatalf("err=%v out=%q stderr=%q calls=%q", err, out, stderr, f.calls(t))
	}
}

func TestMCPInstallExecutablePath(t *testing.T) {
	for _, pathEntry := range []string{"symlink", "different", "absent", "relative"} {
		t.Run(pathEntry, func(t *testing.T) {
			f := installSetup(t, "codex")
			want := f.kioku
			switch pathEntry {
			case "different":
				if err := os.Remove(f.kioku); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f.kioku, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
					t.Fatal(err)
				}
				want = f.exe
			case "absent":
				if err := os.Remove(f.kioku); err != nil {
					t.Fatal(err)
				}
				want = f.exe
			case "relative":
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				f.bin, err = filepath.Rel(cwd, f.bin)
				if err != nil {
					t.Fatal(err)
				}
			}
			if pathEntry == "relative" { // the agent on a relative PATH entry is refused, so check the printed path only
				out, stderr, err := f.run(t, "", "mcp", "install", "codex", "--dry-run")
				if err != nil || !strings.Contains(out, fmt.Sprintf("%q", want)) || f.calls(t) != nil {
					t.Fatalf("%v %s%s calls=%q", err, out, stderr, f.calls(t))
				}
				return
			}
			out, stderr, err := f.run(t, "", "mcp", "install", "codex", "--yes")
			if err != nil {
				t.Fatalf("%v %s%s", err, out, stderr)
			}
			calls := f.calls(t)
			if len(calls) != 1 || calls[0][5] != want || !filepath.IsAbs(calls[0][5]) {
				t.Fatalf("calls=%q want path=%q", calls, want)
			}
		})
	}
}

func TestMCPInstallServerUnchanged(t *testing.T) {
	f := installSetup(t)
	request := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n"
	var want bytes.Buffer
	if err := serveMCP(strings.NewReader(request), &want); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := f.run(t, request, "mcp")
	if err != nil || out != want.String() || stderr != "" {
		t.Fatalf("err=%v out=%q want=%q stderr=%q", err, out, want.String(), stderr)
	}
	if f.calls(t) != nil {
		t.Fatal("server ran an agent")
	}
}

func TestMCPInstallGoRunTemporaryPath(t *testing.T) {
	f := installSetup(t, "codex")
	binary, err := os.ReadFile(f.exe)
	if err != nil {
		t.Fatal(err)
	}
	f.exe = filepath.Join(t.TempDir(), "go-build123", "b001", "exe", "kioku")
	if err := os.MkdirAll(filepath.Dir(f.exe), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.exe, binary, 0700); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := f.run(t, "", "mcp", "install", "codex", "--yes")
	if err == nil || !strings.Contains(stderr, "go run") || f.calls(t) != nil {
		t.Fatalf("err=%v out=%q stderr=%q calls=%q", err, out, stderr, f.calls(t))
	}
}

func TestMCPInstallEscapedSearch(t *testing.T) {
	f := installSetup(t)
	out, stderr, err := f.run(t, "", "--json", "--", "mcp")
	var page struct {
		Total int   `json:"total"`
		Hits  []any `json:"hits"`
	}
	if err != nil || json.Unmarshal([]byte(out), &page) != nil || page.Total != 0 {
		t.Fatalf("err=%v out=%q stderr=%q", err, out, stderr)
	}
	if f.calls(t) != nil {
		t.Fatal("search ran an agent")
	}
}

// An agent executable found only through a relative PATH entry (the current directory) must never run:
// a repository could plant one.
func TestMCPInstallIgnoresCurrentDirectoryAgents(t *testing.T) {
	for _, args := range [][]string{{"mcp", "install", "--yes"}, {"mcp", "install", "codex", "--yes"}} {
		f := installSetup(t, "codex")
		encoded, _ := json.Marshal(args)
		cmd := exec.Command(f.exe, "-test.run=^TestMCPInstallProcess$")
		cmd.Dir = f.bin
		cmd.Env = []string{"PATH=.", "HOME=" + f.home, "INSTALL_LOG=" + f.log, "KIOKU_INSTALL_TEST_CHILD=1", "KIOKU_INSTALL_TEST_ARGS=" + string(encoded), "KIOKU_INDEX=" + filepath.Join(f.home, "index.db")}
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("%v: exit 0, want failure: %s", args, out)
		}
		if got := f.calls(t); got != nil {
			t.Fatalf("%v: ran a current-directory agent: %q", args, got)
		}
	}
}
