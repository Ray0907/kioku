package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestShowOutline(t *testing.T) {
	for _, tc := range []struct {
		name           string
		messages, want []string
	}{
		{"turns and trailing user", []string{
			"user", "question one", "asst", "intermediate", "tool", "tool output",
			"asst", "final one", "tool", "trailing tool", "user", "question two",
			"asst", "intermediate two", "tool", "more output", "asst", "final two",
			"user", "pending question",
		}, []string{"question one", "final one", "question two", "final two", "pending question"}},
		{"ends in tools", []string{"user", "question", "asst", "intermediate", "tool", "output", "asst", "final", "tool", "trailing"}, []string{"question", "final"}},
		{"consecutive users", []string{"user", "first", "tool", "output", "user", "second", "asst", "final"}, []string{"first", "second", "final"}},
		{"only user", []string{"user", "pending"}, []string{"pending"}},
		{"only assistant", []string{"asst", "intermediate", "tool", "output", "asst", "final"}, []string{"final"}},
		{"only tools", []string{"tool", "output"}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := ctxDB(t)
			addSession(t, db, "outline", "pi", "/w/app", time.Hour, tc.messages...)
			key := pageKey{Mode: "show", Ref: "outline", Context: 3, Limit: 2, Outline: true}
			got := []string{}
			offset := 0
			for {
				p, err := compactShow(context.Background(), db, key, offset)
				if err != nil {
					t.Fatal(err)
				}
				if p.Total != len(tc.want) || p.SessionTotal != len(tc.messages)/2 || p.Shown > key.Limit {
					t.Fatalf("unexpected page counts: %+v", p)
				}
				for _, m := range p.Messages {
					if m.Role != "user" && m.Role != "asst" {
						t.Fatalf("outline contains %s", m.Role)
					}
					got = append(got, m.Text)
				}
				if p.Shown > 0 && (tc.messages[2*p.Start+1] != p.Messages[0].Text || tc.messages[2*p.End+1] != p.Messages[p.Shown-1].Text) {
					t.Fatalf("start/end must be original message indices: %+v", p)
				}
				if p.NextCursor == "" {
					break
				}
				plain := key
				plain.Outline = false
				if _, err := plain.offset(p.NextCursor); err == nil {
					t.Fatal("outline cursor accepted without --outline")
				}
				offset, err = key.offset(p.NextCursor)
				if err != nil || offset != len(got) {
					t.Fatalf("cursor offset %d, error %v", offset, err)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOutlineRequiresShow(t *testing.T) {
	doctorHome(t)
	if _, err := runArgs(t, "--json", "--outline"); err == nil || !strings.Contains(err.Error(), "--outline requires show") {
		t.Fatalf("want show-only flag error, got %v", err)
	}
}
