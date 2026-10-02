package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// platformEditor finds a GUI editor app, falling back to Finder.
func platformEditor() (string, []string) {
	for _, a := range []struct{ app, label string }{{"Zed.app", "Zed"}, {"Cursor.app", "Cursor"}, {"Visual Studio Code.app", "Visual Studio Code"}, {"Sublime Text.app", "Sublime Text"}} {
		for _, root := range []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")} {
			if _, e := os.Stat(filepath.Join(root, a.app)); e == nil {
				return a.label, []string{"open", "-a", a.label}
			}
		}
	}
	return "Finder", []string{"open"}
}

func clipboardCmd() *exec.Cmd { return exec.Command("pbcopy") }
