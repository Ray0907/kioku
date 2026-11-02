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
		rows, e := db.QueryContext(ctx, `SELECT `+expr+` v,`+count+` c `+from+`GROUP BY v ORDER BY `+order, args...)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out, other := []facetCount{}, 0
		for rows.Next() {
			var f facetCount
			if e = rows.Scan(&f.Value, &f.Count); e != nil {
				return nil, e
			}
			if len(out) < limit {
				out = append(out, f)
			} else {
				other += f.Count
			}
		}
		if other > 0 {
			out = append(out, facetCount{"(other)", other})
		}
		return out, rows.Err()
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
