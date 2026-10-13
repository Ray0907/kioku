package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func editor() (string, []string) {
	for _, s := range []string{os.Getenv("KIOKU_EDITOR"), os.Getenv("VISUAL")} {
		if s != "" {
			return s, strings.Fields(s)
		}
	}
	for _, s := range []string{"zed", "cursor", "code", "subl"} {
		if p, e := exec.LookPath(s); e == nil {
			return s, []string{p}
		}
	}
	return platformEditor()
}
func terminalEditor(name string) bool {
	switch filepath.Base(name) {
	case "vim", "nvim", "vi", "nano", "hx", "helix", "micro", "kak":
		return true
	}
	return false
}
func openProject(dir string) (*exec.Cmd, bool, string) {
	label, argv := editor()
	if len(argv) == 0 {
		return nil, false, "no editor configured"
	}
	if _, e := os.Stat(dir); e != nil {
		return nil, false, "directory no longer exists: " + dir
	}
	terminal := terminalEditor(argv[0])
	args := argv[1:]
	if terminal {
		switch filepath.Base(argv[0]) {
		case "vim", "nvim", "hx", "helix":
			args = append(args, ".")
		}
	} else {
		args = append(args, dir)
	}
	cmd := exec.Command(argv[0], args...)
	if terminal {
		cmd.Dir = dir
	}
	return cmd, terminal, label
}

type openMsg string

func openStatus(dir, label string, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("%s opened %s in %s", time.Now().Format("15:04"), dir, label)
}
func copyCommand(s string) string {
	var e error
	if cmd := clipboardCmd(); cmd != nil {
		cmd.Stdin = strings.NewReader(s)
		e = cmd.Run()
	}
	// OSC 52 also supports remote terminals; emitted outside Bubble Tea's frame.
	fmt.Fprintf(os.Stderr, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(s)))
	if e != nil {
		return e.Error()
	}
	return fmt.Sprintf("%s copied: %s", time.Now().Format("15:04"), s)
}
