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
