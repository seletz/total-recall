package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// inputKindOut decodes the fields of show --json and search --json this
// feature adds, and the ones the invariants need.
type inputKindOut struct {
	ID        string  `json:"id"`
	EventID   string  `json:"event_id"`
	Actor     string  `json:"actor"`
	InputKind *string `json:"input_kind"`
}

// TestInputKindDemoInvariants checks the numbers on the real --json output
// of the demo archive: the human events of every session, counted by input
// kind plus the unknown ones, add up to all human events (also to the count
// in the database), only human events have a kind, and search reports the
// same kind for an event as show.
func TestInputKindDemoInvariants(t *testing.T) {
	e, db := chartEnv(t)
	st, _ := fixturedb.Demo(t)
	var sessions []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(e.mustRun(t, "--db", db, "sessions", "--json")), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) < 100 {
		t.Fatalf("sessions = %d, the demo archive is smaller than expected", len(sessions))
	}
	byKind := map[string]int{}
	human, unknown := 0, 0
	kindOf := map[string]string{}
	for _, s := range sessions {
		var shown struct {
			Events []inputKindOut `json:"events"`
		}
		if err := json.Unmarshal([]byte(e.mustRun(t, "--db", db, "show", s.ID, "--all", "--json")), &shown); err != nil {
			t.Fatalf("show %s: %v", s.ID, err)
		}
		for _, ev := range shown.Events {
			if ev.Actor != string(model.ActorHuman) {
				if ev.InputKind != nil {
					t.Fatalf("%s: %s event has input_kind %q", ev.ID, ev.Actor, *ev.InputKind)
				}
				continue
			}
			human++
			if ev.InputKind == nil {
				unknown++
				continue
			}
			if !model.InputKind(*ev.InputKind).Valid() {
				t.Fatalf("%s: invalid input_kind %q", ev.ID, *ev.InputKind)
			}
			byKind[*ev.InputKind]++
			kindOf[ev.ID] = *ev.InputKind
		}
	}
	sum := unknown
	for _, n := range byKind {
		sum += n
	}
	if sum != human || human == 0 {
		t.Errorf("kinds add up to %d, human events are %d", sum, human)
	}
	// The demo has typed, pasted and remote prompts; the ingest knows all of them.
	for _, k := range []string{"typed", "pasted", "remote"} {
		if byKind[k] == 0 {
			t.Errorf("no %s event in the demo archive: %v", k, byKind)
		}
	}
	if unknown != 0 {
		t.Errorf("%d human events without input_kind in a freshly ingested archive", unknown)
	}
	// The totals agree with the database.
	for k, n := range byKind {
		_, rows, err := st.Query(context.Background(), "SELECT count(*) FROM events WHERE actor = 'human' AND input_kind = '"+k+"'")
		if err != nil || len(rows) != 1 {
			t.Fatalf("count %s: %v", k, err)
		}
		if got := rows[0][0]; got != int64(n) {
			t.Errorf("show reports %d %s events, the database has %v", n, k, got)
		}
	}
	// search --json shows the same kind as show for the same event.
	var hits []inputKindOut
	if err := json.Unmarshal([]byte(e.mustRun(t, "--db", db, "search", "--json", "--actor", "human", "lint")), &hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("no search hits")
	}
	for _, h := range hits {
		want := kindOf[h.EventID]
		got := ""
		if h.InputKind != nil {
			got = *h.InputKind
		}
		if got != want {
			t.Errorf("search %s: input_kind %q, show has %q", h.EventID, got, want)
		}
	}
	// The text output of show names the kind next to the actor.
	out := e.mustRun(t, "--db", db, "show", "claude:7d1e4c20-0001-4000-8000-000000000001")
	if !strings.Contains(out, "human (remote)") {
		t.Errorf("show text lacks the input kind:\n%s", out)
	}
}

// TestMCPInputKindAddsUp pages the show tool of every demo session with a
// small bound: over all pages the input kinds plus the unknown ones add up
// to the human events, only human events carry a kind, and the event count
// of the pages equals the CLI output.
func TestMCPInputKindAddsUp(t *testing.T) {
	p := newPagedDemo(t, 4<<10)
	list, _, _ := newPagedDemo(t, 16<<20).call("sessions", map[string]any{"limit": 500})
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(list), &rows); err != nil || len(rows) == 0 {
		t.Fatalf("sessions: %v", err)
	}
	type ev struct {
		ID        string  `json:"event_id"`
		Actor     string  `json:"actor"`
		InputKind *string `json:"input_kind"`
	}
	kinds := map[string]int{}
	human, unknown, paged := 0, 0, 0
	for _, r := range rows {
		var cliOut struct{ Events []ev }
		if err := json.Unmarshal([]byte(p.cli("show", r.ID, "--all", "--limit=1000")), &cliOut); err != nil {
			t.Fatalf("show %s: %v", r.ID, err)
		}
		pages, _ := p.crawl("show", map[string]any{"session": r.ID, "all": true, "limit": 1000})
		n := 0
		for _, pg := range pages {
			var d struct{ Events []ev }
			if err := json.Unmarshal([]byte(pg), &d); err != nil {
				t.Fatalf("show %s page: %v", r.ID, err)
			}
			for _, e := range d.Events {
				n++
				switch {
				case e.Actor != "human" && e.InputKind != nil:
					t.Errorf("%s: %s event has input_kind %q", r.ID, e.Actor, *e.InputKind)
				case e.Actor == "human" && e.InputKind == nil:
					human++
					unknown++
				case e.Actor == "human":
					human++
					kinds[*e.InputKind]++
				}
			}
		}
		if n != len(cliOut.Events) {
			t.Errorf("%s: pages hold %d events, CLI %d", r.ID, n, len(cliOut.Events))
		}
		paged += n
	}
	sum := unknown
	for _, c := range kinds {
		sum += c
	}
	if sum != human || human == 0 || paged == 0 {
		t.Errorf("kinds %v + %d unknown = %d, human events %d", kinds, unknown, sum, human)
	}
	for _, k := range []string{"typed", "pasted", "remote"} {
		if kinds[k] == 0 {
			t.Errorf("no %s event over MCP: %v", k, kinds)
		}
	}
}
