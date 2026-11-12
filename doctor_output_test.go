package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDoctorOutput(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("json=%v", jsonMode), func(t *testing.T) {
			doctorHome(t)
			args := []string{}
			if jsonMode {
				args = append(args, "--json")
			}
			out, err := doctorRun(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "\x1b") {
				t.Fatalf("unexpected ANSI in non-TTY/JSON output: %q", out)
			}
			if jsonMode {
				var checks []map[string]any
				if err := json.Unmarshal([]byte(out), &checks); err != nil || len(checks) != 10 {
					t.Fatalf("JSON shape: %q, err=%v", out, err)
				}
				for _, c := range checks {
					_, nameOK := c["name"].(string)
					_, okOK := c["ok"].(bool)
					_, optionalOK := c["optional"].(bool)
					_, detailOK := c["detail"].(string)
					if len(c) != 4 || !nameOK || !okOK || !optionalOK || !detailOK {
						t.Fatalf("wrong check shape: %v", c)
					}
				}
			} else {
				lines := strings.Split(strings.TrimSpace(out), "\n")
				if len(lines) != 10 || !strings.HasPrefix(lines[0], "- Claude Code  not found (optional:") || !strings.HasPrefix(lines[6], "- Hermes Agent  not found (optional:") || !strings.HasPrefix(lines[7], "- Antigravity CLI  not found (optional:") || !strings.HasPrefix(lines[9], "✓ sqlite  ") {
					t.Fatalf("text shape: %q", out)
				}
			}
		})
	}
}
