package main

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"
)

func showFixturePage(t *testing.T, ref string, ctxN int, query string, full bool) showPage {
	t.Helper()
	db := ctxDB(t)
	addSession(t, db, "s1", "claude", "/w/app", time.Hour,
		"user", "fix the login redirect", "asst", "on it\nsecond line with `code`", "user", "push it", "asst", "Pushed to main.")
	p, e := compactShow(context.Background(), db, pageKey{Mode: "show", Ref: ref, Context: ctxN, Limit: 10, Query: query, Full: full}, 0)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func capture(t *testing.T, f func() error) string {
	t.Helper()
	old := output
	defer func() { output = old }()
	var b bytes.Buffer
	output = &b
	if e := f(); e != nil {
		t.Fatal(e)
	}
	return b.String()
}

func TestShowTextAndJSONUnchanged(t *testing.T) {
	p := showFixturePage(t, "s1:1", 1, "", false)
	text := capture(t, func() error { return renderShow(p, false) })
	wantText := "claude · app · /w/app · 2026-10-23\nresume: claude --resume s1\ntopic: fix the login redirect\nmessages 0–2 of 4 · hit 1\n  10:56 user: fix the login redirect\n> 10:57 asst: on it second line with `code`\n  10:58 user: push it\n"
	if text != wantText {
		t.Fatalf("text changed:\n%q", text)
	}
	js := capture(t, func() error { return renderShow(p, true) })
	wantJSON := `{"harness":"claude","project":"app","topic":"fix the login redirect","cwd":"/w/app","date":"2026-10-23","resume_cmd":"claude --resume s1","resume_argv":["claude","--resume","s1"],"shown":3,"total":3,"start":0,"end":2,"session_total":4,"hit_index":1,"messages":[{"time":"10:56","role":"user","text":"fix the login redirect","hit":false,"matches":false},{"time":"10:57","role":"asst","text":"on it second line with ` + "`code`" + `","hit":true,"matches":false},{"time":"10:58","role":"user","text":"push it","hit":false,"matches":false}]}` + "\n"
	if js != wantJSON {
		t.Fatalf("json changed:\n%q", js)
	}
}

func TestShowMarkdown(t *testing.T) {
	p := showFixturePage(t, "s1:1", 1, "", true)
	got := capture(t, func() error { return renderShowMD(p) })
	want := "### fix the login redirect\n\nclaude · app · `/w/app` · 2026-10-23 · messages 0–2 of 4 · hit 1\n\nResume: `claude --resume s1`\n\n**user** 10:56\n> fix the login redirect\n\n**asst** 10:57 · hit\n> on it\n> second line with `code`\n\n**user** 10:58\n> push it\n"
	if got != want {
		t.Fatalf("markdown:\n%q", got)
	}
}

func TestShowFormatFlag(t *testing.T) {
	doctorHome(t)
	for _, args := range [][]string{
		{"show", "x:0", "--format", "html"},
		{"show", "x:0", "--format", "md", "--json"},
		{"--format", "md", "query"},
	} {
		if _, e := runArgs(t, args...); e == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func runArgs(t *testing.T, args ...string) (string, error) {
	t.Helper()
	oldArgs, oldOutput := os.Args, output
	defer func() { os.Args, output = oldArgs, oldOutput }()
	var b bytes.Buffer
	os.Args, output = append([]string{"kioku"}, args...), &b
	e := run()
	return b.String(), e
}
