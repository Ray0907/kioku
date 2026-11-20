package main

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
)

func queryCmd(ctx context.Context, db *sql.DB, q, h string, projects []string, limit, rev int, allTime, includeSelf, includeAutomated bool, rng timeRange, root ...string) tea.Cmd {
	return func() tea.Msg {
		k := pageKey{Query: q, Harness: h, Projects: projects, Limit: limit, AllTime: allTime, IncludeSelf: includeSelf, IncludeAutomated: includeAutomated, Since: rng.Since, Until: rng.Until}
		if len(root) > 0 {
			k.Root = root[0]
		}
		rows, e := search(ctx, db, k)
		if e != nil {
			return resultMsg{revision: rev, err: e}
		}
		total, sessions, e := searchTotals(ctx, db, k, len(rows))
		return resultMsg{revision: rev, rows: rows, total: total, totalSessions: sessions, err: e}
	}
}

func (m *model) requestQuery() tea.Cmd {
	if m.fresh > 0 { // this query reads the data a background sync added
		m.loadedKey = ""
	}
	m.fresh = 0
	if m.cancel != nil {
		m.cancel()
	}
	if m.loadCancel != nil {
		m.loadCancel()
	}
	m.loadRevision++
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	return queryCmd(ctx, m.db, m.q, m.harness, m.projects, m.limit, m.revision, m.allTime, m.includeSelf, m.includeAutomated, m.rng, m.root)
}

// loadKey names what the preview shows: the same session under the same query
// has the same messages and hits, so there is nothing to reload.
func (m model) loadKey(h hit) string {
	return h.UID + "\x00" + m.q + "\x00" + strconv.FormatBool(m.includeSelf)
}

func (m *model) requestLoad() tea.Cmd {
	if m.loadCancel != nil {
		m.loadCancel()
	}
	m.loadRevision++
	m.transcriptOffset = 0
	m.cursor = -1
	if m.sel >= len(m.rows) {
		m.sel = max(0, len(m.rows)-1)
	}
	if m.sel < m.offset {
		m.offset = m.sel
	}
	listH := max(1, m.listHeight())
	if m.sel >= m.offset+listH {
		m.offset = m.sel - listH + 1
	}
	if len(m.rows) == 0 {
		m.messages, m.hitIndex, m.loadedKey = nil, map[int]bool{}, ""
		return nil
	}
	selected := m.rows[m.sel]
	m.cursor = selected.Index
	key := m.loadKey(selected)
	if key == m.loadedKey {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.loadCancel = cancel
	rev := m.loadRevision
	q := m.q
	includeSelf := m.includeSelf
	return func() tea.Msg {
		msgs, e := transcript(ctx, m.db, selected.UID)
		hits := map[int]bool{}
		if e == nil && toFTS(q) != "" {
			rows, err := m.db.QueryContext(ctx, `SELECT m.idx FROM messages_fts JOIN messages m ON m.id=messages_fts.rowid WHERE messages_fts MATCH ? AND m.session_id=(SELECT session_id FROM sessions WHERE uid=?) `+selfFilter(includeSelf), toFTS(q), selected.UID)
			if err == nil {
				for rows.Next() {
					var i int
					if rows.Scan(&i) == nil {
						hits[i] = true
					}
				}
				e = rows.Err()
				rows.Close()
			} else {
				e = err
			}
		}
		return loadMsg{rev, key, msgs, hits, e}
	}
}

func (m *model) refresh() tea.Cmd {
	m.revision++
	if m.cancel != nil {
		m.cancel()
	}
	if m.loadCancel != nil {
		m.loadCancel()
	}
	m.loadRevision++
	m.loadedKey = "" // the preview keeps showing until the new results replace it; blanking it every keystroke made the screen flash
	m.slow = false
	rev := m.revision
	return tea.Batch(
		tea.Tick(30*time.Millisecond, func(time.Time) tea.Msg { return debounce(rev) }),
		tea.Tick(slowAfter, func(time.Time) tea.Msg { return slowMsg(rev) }),
	)
}
