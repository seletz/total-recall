package cli

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// coordinatorTask is the text the demo coordinator sends to its worker.
const coordinatorTask = "Behebe den Lint-Fehler in route/cache.go"

type workSeconds struct {
	Days []struct {
		Day        string
		Raw        int `json:"raw_seconds"`
		Categories []struct {
			Raw int `json:"raw_seconds"`
		}
	}
}

type workdayBlocks struct {
	Days []struct {
		Span         int `json:"span_seconds"`
		BlockSeconds int `json:"block_seconds"`
		BreakSeconds int `json:"break_seconds"`
		Blocks       []struct {
			Seconds int
		}
	}
}

// TestCoordinatorPromptsDemo is the invariant of #265 on the real --json of
// the demo archive. The archive has a coordinator that sends a task to a
// worker with `herdr agent prompt`; the worker's inputs are automation, not
// human. Setting that event back to human (the archive as an older version
// made it) and running `reindex --actors` gives the archive back: the human
// events and the working time fall by exactly the reclassified event, and the
// sums of workday and worktime still equal their parts.
func TestCoordinatorPromptsDemo(t *testing.T) {
	t.Parallel()
	checkDispatchedInvariant(t, dispatchedCase{
		ids:       "text LIKE '" + coordinatorTask + "%' OR text LIKE 'Rebase den Branch%'",
		n:         2,
		day:       "2026-10-29",
		stayHuman: []string{"text LIKE 'Danke. Trag bitte%'", "text LIKE 'Gib den Lint-Fix%'"},
	})
}

// TestSpawnedPromptsDemo is the same invariant for #269: a coordinator
// starts a worker in a worktree and hands it a prompt from a file, so no
// argument holds the text. The worker's first input is automation (the start
// command names its directory), its later input stays human.
func TestSpawnedPromptsDemo(t *testing.T) {
	t.Parallel()
	checkDispatchedInvariant(t, dispatchedCase{
		ids:       "text LIKE 'Ergänze den GPX-Export analog%'",
		n:         1,
		day:       "2026-10-25",
		stayHuman: []string{"text LIKE 'Danke, passt so.'"},
	})
}

// TestSpawnedThroughFunctionDemo is the same for a worker started with a shell
// function: the start and prompt commands are only in the definition.
func TestSpawnedThroughFunctionDemo(t *testing.T) {
	t.Parallel()
	checkDispatchedInvariant(t, dispatchedCase{
		ids:       "text LIKE 'Schreibe die Doku zum GPX-Format%'",
		n:         1,
		day:       "2026-10-18",
		stayHuman: []string{"text LIKE 'Passt, danke.'"},
	})
}

type dispatchedCase struct {
	ids       string   // SQL condition: the events another session sent, stored as automation
	n         int      // how many there are
	day       string   // the day the invariant is checked on
	stayHuman []string // SQL conditions of human events that must stay human, one each
}

func checkDispatchedInvariant(t *testing.T, c dispatchedCase) {
	t.Helper()
	e, db := chartEnv(t)
	st, _ := fixturedb.Demo(t)
	ctx := t.Context()
	_, rows, err := st.Query(ctx, "SELECT id FROM events WHERE actor = 'automation' AND ("+c.ids+")")
	if err != nil || len(rows) != c.n {
		t.Fatalf("dispatched events = %v, %v; want %d", rows, err, c.n)
	}
	// Control cases stay human.
	for _, cond := range c.stayHuman {
		q := "SELECT count(*) FROM events WHERE actor = 'human' AND " + cond
		if _, r, err := st.Query(ctx, q); err != nil || r[0][0] != int64(1) {
			t.Fatalf("%s = %v, %v; want 1", q, r, err)
		}
	}

	// The archive as an older version stored it: the events are human again.
	old := filepath.Join(t.TempDir(), "old.db")
	b, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, b, 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := raw.Exec("UPDATE events SET actor = 'human', input_kind = 'typed' WHERE id = ?", r[0]); err != nil {
			t.Fatal(err)
		}
	}
	_ = raw.Close()

	day := []string{"--since", c.day, "--until", c.day}
	humans := func(db string) int {
		return decode[ActivitySeriesJSON](t, e.mustRun(t, append([]string{"--db", db, "stats", "--json", "--period", "day"}, day...)...)).Total.Human
	}
	work := func(db string) (total int, days workSeconds) {
		out := e.mustRun(t, append([]string{"--db", db, "stats", "--worktime", "--json"}, day...)...)
		days = decode[workSeconds](t, out)
		for _, d := range days.Days {
			cats := 0
			for _, c := range d.Categories {
				cats += c.Raw
			}
			if cats != d.Raw {
				t.Errorf("%s: categories add up to %d, day has %d", d.Day, cats, d.Raw)
			}
			total += d.Raw
		}
		return total, days
	}
	span := func(db string) int {
		w := decode[workdayBlocks](t, e.mustRun(t, append([]string{"--db", db, "stats", "--workday", "--blocks", "--json"}, day...)...))
		total := 0
		for _, d := range w.Days {
			parts := 0
			for _, b := range d.Blocks {
				parts += b.Seconds
			}
			if parts != d.BlockSeconds || d.BlockSeconds+d.BreakSeconds != d.Span {
				t.Errorf("workday: blocks %d (sum %d) + breaks %d != span %d", d.BlockSeconds, parts, d.BreakSeconds, d.Span)
			}
			total += d.Span
		}
		return total
	}

	hNow, hOld := humans(db), humans(old)
	wNow, _ := work(db)
	wOld, _ := work(old)
	sNow, sOld := span(db), span(old)
	if hOld != hNow+c.n {
		t.Errorf("human events: %d before, %d after; want exactly %d fewer", hOld, hNow, c.n)
	}
	if wOld <= wNow || sOld < sNow {
		t.Errorf("working time %d s -> %d s, workday span %d s -> %d s; the reclassified event must not count", wOld, wNow, sOld, sNow)
	}

	out := e.mustRun(t, "--db", old, "reindex", "--actors")
	if want := fmt.Sprintf("human -> automation: %d", c.n); !strings.Contains(out, want) || !strings.Contains(out, fmt.Sprintf("%d of them were typed in by another session", c.n)) {
		t.Errorf("reindex --actors: %s", out)
	}
	if h, w := humans(old), func() int { v, _ := work(old); return v }(); h != hNow || w != wNow || span(old) != sNow {
		t.Errorf("after reindex: human %d work %d, want %d and %d", h, w, hNow, wNow)
	}
	if again := e.mustRun(t, "--db", old, "reindex", "--actors"); !strings.Contains(again, ", 0 changed") {
		t.Errorf("second reindex --actors changes more: %s", again)
	}
}
