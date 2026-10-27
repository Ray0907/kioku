package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Stored timestamps are free-form RFC3339 strings (Z or offsets, any fraction
// length). datetime() parses all of them to UTC seconds, so bounds use the same
// "YYYY-MM-DD HH:MM:SS" form and the interval is half-open: since <= ts < until.
// Unparseable or empty stamps give NULL and never match a range.
const boundLayout = "2006-01-02 15:04:05"

type timeRange struct{ Since, Until string }

func (r timeRange) active() bool { return r.Since != "" || r.Until != "" }

// rangePredicate filters messages aliased as m.
func rangePredicate(r timeRange) (string, []any) {
	if !r.active() {
		return "", nil
	}
	clause, args := "", []any{}
	if r.Since != "" {
		clause += `AND datetime(m.ts)>=? `
		args = append(args, r.Since)
	}
	if r.Until != "" {
		clause += `AND datetime(m.ts)<? `
		args = append(args, r.Until)
	}
	return clause, args
}

func localMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// parseBound turns one --since/--until value into a UTC bound. Day-level forms
// resolve to local midnight, so a repeated call gives the same bound all day
// and cursors stay valid. A date as --until covers that whole day.
func parseBound(value string, until bool, now time.Time) (string, error) {
	day := localMidnight(now)
	var at time.Time
	switch v := strings.ToLower(value); {
	case v == "today":
		at = day
		if until {
			at = day.AddDate(0, 0, 1)
		}
	case v == "yesterday":
		at = day.AddDate(0, 0, -1)
		if until {
			at = day
		}
	case len(v) > 1 && (v[len(v)-1] == 'd' || v[len(v)-1] == 'w'):
		n, e := strconv.Atoi(v[:len(v)-1])
		if e != nil || n < 0 || n > 36500 {
			return "", fmt.Errorf("invalid %s", value)
		}
		if v[len(v)-1] == 'w' {
			n *= 7
		}
		at = day.AddDate(0, 0, -n)
	default:
		if t, e := time.ParseInLocation("2006-01-02", value, now.Location()); e == nil {
			at = t
			if until {
				at = t.AddDate(0, 0, 1)
			}
		} else if t, e := time.Parse(time.RFC3339Nano, value); e == nil {
			at = t
		} else {
			return "", fmt.Errorf("invalid %s", value)
		}
	}
	return at.UTC().Format(boundLayout), nil
}

const rangeForms = "use YYYY-MM-DD, RFC3339, Nd, Nw, today or yesterday"

func resolveRange(since, until string, sinceSet, untilSet bool, now time.Time) (timeRange, error) {
	var r timeRange
	var e error
	if sinceSet {
		if r.Since, e = parseBound(since, false, now); e != nil {
			return r, fmt.Errorf("--since: %w (%s)", e, rangeForms)
		}
	}
	if untilSet {
		if r.Until, e = parseBound(until, true, now); e != nil {
			return r, fmt.Errorf("--until: %w (%s)", e, rangeForms)
		}
	}
	if r.Since != "" && r.Until != "" && r.Since >= r.Until {
		return r, fmt.Errorf("--since must be before --until")
	}
	return r, nil
}
