package main

import (
	"fmt"
	"strings"
	"testing"
)

func benchModel(msgs int, full bool) model {
	m := model{width: 140, height: 45, pal: dark, focus: false, full: full, q: "checkout", hitIndex: map[int]bool{}}
	for i := 0; i < 60; i++ {
		m.rows = append(m.rows, hit{Harness: "claude", Project: "lumen-web", TS: "2026-10-07T07:20:00Z", CWD: "/tmp", Snippet: "The checkout page double-submits when you tap Pay twice on a slow connection."})
	}
	long := strings.Repeat("The checkout page 結帳按鈕 double-submits when you tap Pay twice on a slow connection. ", 5)
	for i := 0; i < msgs; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		m.messages = append(m.messages, message{Index: i, TS: fmt.Sprintf("2026-10-07T%02d:%02d:00Z", 7+i/60%12, i%60), Role: role, Text: long})
		if i%7 == 0 {
			m.hitIndex[i] = true
		}
	}
	m.cursor = msgs / 2
	return m
}

// Frame cost of View on a wide terminal; run with: go test -tags sqlite_fts5 -run XXX -bench View -benchmem
func BenchmarkViewFolded200(b *testing.B) {
	m := benchModel(200, false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}
func BenchmarkViewFolded5000(b *testing.B) {
	m := benchModel(5000, false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}
func BenchmarkViewFull5000(b *testing.B) {
	m := benchModel(5000, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}
