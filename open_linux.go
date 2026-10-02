package main

import (
	"os"
	"os/exec"
)

// platformEditor falls back to the desktop's default opener.
func platformEditor() (string, []string) {
	if p, e := exec.LookPath("xdg-open"); e == nil {
		return "xdg-open", []string{p}
	}
	return "", nil
}

// clipboardCmd picks the first clipboard tool for the session; nil leaves OSC 52 alone.
func clipboardCmd() *exec.Cmd {
	for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
		if c[0] == "wl-copy" && os.Getenv("WAYLAND_DISPLAY") == "" {
			continue
		}
		if p, e := exec.LookPath(c[0]); e == nil {
			return exec.Command(p, c[1:]...)
		}
	}
	return nil
}
