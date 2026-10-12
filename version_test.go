package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	info := func(v string) *debug.BuildInfo { return &debug.BuildInfo{Main: debug.Module{Version: v}} }
	for _, c := range []struct {
		name, ldflags, want string
		info                *debug.BuildInfo
	}{
		{"ldflags win", "v0.4.0", "v0.4.0", info("v9.9.9")},
		{"go install", "dev", "v0.4.0", info("v0.4.0")},
		{"local go build", "dev", "dev", info("(devel)")},
		{"empty build info", "dev", "dev", info("")},
		{"no build info", "dev", "dev", nil},
	} {
		if got := resolveVersion(c.ldflags, c.info); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
