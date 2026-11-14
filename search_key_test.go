package main

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"testing"
)

func checkSearchKey(t *testing.T, db *sql.DB, k pageKey, want string) {
	t.Helper()
	ctx := context.Background()
	rows, err := search(ctx, db, k)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := newestMatches(ctx, db, k, toFTS(k.Query), nil, false, k.Limit)
	if err != nil {
		t.Fatal(err)
	}
	newest, err := newestPageHits(ctx, db, ids)
	if err != nil {
		t.Fatal(err)
	}
	for path, hits := range map[string][]hit{"search": rows, "newestMatches": newest} {
		var sessions []string
		for _, h := range hits {
			sessions = append(sessions, h.SessionID)
		}
		sort.Strings(sessions)
		if got := strings.Join(sessions, ","); got != want {
			t.Errorf("%s: got %q, want %q", path, got, want)
		}
	}
}

func TestSearchKeyInclusionFlags(t *testing.T) {
	automatedFixture(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	addSession(t, db, "self", "codex", "/w/app", 0, "user", "b7needle self message")
	if _, err := db.Exec(`UPDATE messages SET self=1 WHERE session_uid='self'`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                   string
		includeSelf, automated bool
		want                   string
	}{
		{"default", false, false, "normal,quote"},
		{"self", true, false, "normal,quote,self"},
		{"automated", false, true, "guidelines,normal,quote,review"},
		{"both", true, true, "guidelines,normal,quote,review,self"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkSearchKey(t, db, pageKey{Query: "b7needle", Limit: 20, IncludeSelf: tc.includeSelf, IncludeAutomated: tc.automated}, tc.want)
		})
	}
}

func TestSearchKeyRangeAndRoot(t *testing.T) {
	db := rangeCorpus(t)
	for _, tc := range []struct {
		name, since, root, want string
	}{
		{"unscoped", "", "", "new,old"},
		{"since", lastTwentyDays.Since, "", "new"},
		{"root", "", "/w/app", "new"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkSearchKey(t, db, pageKey{Query: "login", Limit: 20, Since: tc.since, Root: tc.root}, tc.want)
		})
	}
}
