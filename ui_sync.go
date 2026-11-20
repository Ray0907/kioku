package main

import (
	"database/sql"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

// armTick schedules the next background sync unless syncing is off or one is
// already scheduled, so there is never more than one timer chain.
func (m *model) armTick() tea.Cmd {
	if syncEvery <= 0 || m.tick {
		return nil
	}
	m.tick = true
	return tea.Tick(syncEvery, func(time.Time) tea.Msg { return syncTick{} })
}

// startSync does nothing until the runtime runs the command, so tests that
// build model updates never touch the real index.
func startSync(periodic bool) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan syncMsg, 32)
		go func() {
			db, err := openDB()
			if err != nil {
				ch <- syncMsg{done: true, err: err, periodic: periodic}
				close(ch)
				return
			}
			defer db.Close()
			added, err := syncAdded(db, func(i, n int) {
				if periodic {
					return
				}
				if i == 1 || i == n || i%25 == 0 {
					select {
					case ch <- syncMsg{progress: fmt.Sprintf("indexing %d/%d", i, n), pct: i * 100 / max(1, n)}:
					default:
					}
				}
			})
			ch <- syncMsg{done: true, err: err, periodic: periodic, added: added}
			close(ch)
		}()
		return waitSync(ch)()
	}
}

// syncAdded syncs the index and returns how many messages it gained.
// syncStats.Messages holds per-agent totals, so the gain is the difference.
func syncAdded(db *sql.DB, progress func(int, int)) (int, error) {
	var before int
	if err := db.QueryRow("SELECT coalesce(sum(msg_count),0) FROM sessions").Scan(&before); err != nil {
		return 0, err
	}
	stats, err := syncIndex(db, false, progress)
	after := 0
	for _, n := range stats.Messages {
		after += n
	}
	return max(0, after-before), err
}

func waitSync(ch <-chan syncMsg) tea.Cmd {
	return func() tea.Msg {
		x, ok := <-ch
		if !ok {
			return nil
		}
		return syncEnvelope{x, ch}
	}
}
