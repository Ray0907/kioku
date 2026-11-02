package main

import (
	"testing"
	"time"
)

func TestParseRangeSpec(t *testing.T) {
	taipei := time.FixedZone("Taipei", 8*3600)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, taipei)
	cases := []struct {
		spec string
		want timeRange
	}{
		{"", timeRange{}},
		{"7d", timeRange{Since: "2026-10-15 16:00:00"}},
		{"2w", timeRange{Since: "2026-10-08 16:00:00"}},
		{"today", timeRange{Since: "2026-10-22 16:00:00"}},
		{"2026-10-07..2026-10-07", timeRange{Since: "2026-10-06 16:00:00", Until: "2026-10-07 16:00:00"}},
		{" 2026-10-01 .. yesterday ", timeRange{Since: "2026-09-30 16:00:00", Until: "2026-10-22 16:00:00"}},
		{"..2026-10-07", timeRange{Until: "2026-10-07 16:00:00"}},
	}
	for _, c := range cases {
		got, e := parseRangeSpec(c.spec, now)
		if e != nil || got != c.want {
			t.Errorf("%q: got %+v err=%v, want %+v", c.spec, got, e, c.want)
		}
	}
	for _, bad := range []string{"bogus", "2026-10-07..2026-10-01", "7d..bogus", "1..2..3"} {
		if _, e := parseRangeSpec(bad, now); e == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestRangeLabel(t *testing.T) {
	old := time.Local
	time.Local = time.FixedZone("Taipei", 8*3600)
	defer func() { time.Local = old }()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.Local)
	for spec, want := range map[string]string{
		"":                       "",
		"7d":                     "from 2026-10-16",
		"..2026-10-07":           "to 2026-10-07",
		"2026-10-07..2026-10-07": "2026-10-07",
		"2026-10-01..2026-10-07": "2026-10-01 to 2026-10-07",
		"2026-10-01T09:30:00Z..": "from 2026-10-01 17:30",
	} {
		r, e := parseRangeSpec(spec, now)
		if e != nil {
			t.Fatalf("%q: %v", spec, e)
		}
		if got := r.label(); got != want {
			t.Errorf("%q: label %q, want %q", spec, got, want)
		}
	}
}
