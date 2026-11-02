package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type facetCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}
type facets struct {
	Project []facetCount `json:"project"`
	Harness []facetCount `json:"harness"`
	Week    []facetCount `json:"week,omitempty"`
}

// Counts per project, harness and week (local time, starting Monday, like
// --since/--until) over the same FROM clause as the page. It needs the
// sessions table joined as s. Groups beyond the limit are summed into
// "(other)", so every list adds up to the total.
func facetCounts(ctx context.Context, db *sql.DB, from string, args []any, distinct, weeks bool) (*facets, error) {
	count := `count(*)`
	if distinct {
		count = `count(DISTINCT m.session_uid)`
	}
	group := func(expr, order string, limit int) ([]facetCount, error) {
		return groupLimited(ctx, db, `SELECT `+expr+` v,`+count+` c `+from+`GROUP BY v ORDER BY `+order, args, limit,
			func(r *sql.Rows) (f facetCount, e error) { e = r.Scan(&f.Value, &f.Count); return },
			func(rest []facetCount) facetCount {
				other := facetCount{Value: "(other)"}
				for _, f := range rest {
					other.Count += f.Count
				}
				return other
			})
	}
	var f facets
	var e error
	if f.Project, e = group(`s.project`, `c DESC,v`, 20); e != nil {
		return nil, e
	}
	if f.Harness, e = group(`s.harness`, `c DESC,v`, 20); e != nil {
		return nil, e
	}
	if weeks {
		if f.Week, e = group(`coalesce(date(m.ts,'localtime','weekday 0','-6 days'),'(unknown)')`, `v DESC`, 52); e != nil {
			return nil, e
		}
	}
	return &f, nil
}

// groupLimited keeps the first limit rows of a grouped query and folds the rest
// into one extra row, so every list still adds up to the total.
func groupLimited[T any](ctx context.Context, db *sql.DB, query string, args []any, limit int, scan func(*sql.Rows) (T, error), fold func([]T) T) ([]T, error) {
	rows, e := db.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out, rest := []T{}, []T{}
	for rows.Next() {
		x, e := scan(rows)
		if e != nil {
			return nil, e
		}
		if len(out) < limit {
			out = append(out, x)
		} else {
			rest = append(rest, x)
		}
	}
	if len(rest) > 0 {
		out = append(out, fold(rest))
	}
	return out, rows.Err()
}
func renderFacets(f *facets) {
	if f == nil {
		return
	}
	line := func(name string, list []facetCount) {
		if len(list) == 0 {
			return
		}
		parts := make([]string, len(list))
		for i, x := range list {
			parts[i] = fmt.Sprintf("%s %d", x.Value, x.Count)
		}
		fmt.Fprintf(output, "%s: %s\n", name, strings.Join(parts, " · "))
	}
	line("by project", f.Project)
	line("by harness", f.Harness)
	line("by week", f.Week)
}
