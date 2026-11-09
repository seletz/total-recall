package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/demo"
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
	// One timeline run lists every event of the archive with its kind; show
	// is then compared for the sessions that have a kind other than typed.
	all := decode[[]struct {
		inputKindOut
		SessionID string `json:"session_id"`
	}](t, e.mustRun(t, "--db", db, "timeline", "--json", "--actor", "all", "--all", "--limit", "0", "--since", "2000-01-01", "--until", "2100-01-01"))
	if len(all) < 1000 {
		t.Fatalf("events = %d, the demo archive is smaller than expected", len(all))
	}
	byKind := map[string]int{}
	human, unknown := 0, 0
	kindOf := map[string]string{}
	special := map[string]bool{}
	for _, ev := range all {
		if ev.Actor != string(model.ActorHuman) {
			if ev.InputKind != nil {
				t.Fatalf("%s: %s event has input_kind %q", ev.EventID, ev.Actor, *ev.InputKind)
			}
			continue
		}
		human++
		if ev.InputKind == nil {
			unknown++
			continue
		}
		if !model.InputKind(*ev.InputKind).Valid() {
			t.Fatalf("%s: invalid input_kind %q", ev.EventID, *ev.InputKind)
		}
		byKind[*ev.InputKind]++
		kindOf[ev.EventID] = *ev.InputKind
		if *ev.InputKind != "typed" {
			special[ev.SessionID] = true
		}
	}
	// show reports the same kind for every event of those sessions.
	for id := range special {
		var shown struct {
			Events []inputKindOut `json:"events"`
		}
		if err := json.Unmarshal([]byte(e.mustRun(t, "--db", db, "show", id, "--all", "--json")), &shown); err != nil {
			t.Fatalf("show %s: %v", id, err)
		}
		for _, ev := range shown.Events {
			got := ""
			if ev.InputKind != nil {
				got = *ev.InputKind
			}
			if got != kindOf[ev.ID] && ev.Actor == "human" {
				t.Errorf("show %s: input_kind %q, timeline has %q", ev.ID, got, kindOf[ev.ID])
			}
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
			t.Errorf("timeline reports %d %s events, the database has %v", n, k, got)
		}
	}
	// search --json shows the same kind as timeline for the same event.
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
			t.Errorf("search %s: input_kind %q, timeline has %q", h.EventID, got, want)
		}
	}
	// The text output of show names the kind next to the actor.
	out := e.mustRun(t, "--db", db, "show", "claude:7d1e4c20-0001-4000-8000-000000000001")
	if !strings.Contains(out, "human (remote)") {
		t.Errorf("show text lacks the input kind:\n%s", out)
	}
}

// TestMCPInputKindAddsUp pages the show tool of the demo sessions that have
// a kind other than typed (a handful, about three pages each): over all
// pages the input kinds plus the unknown ones add up to the human events,
// only human events carry a kind, and the pages hold the events of the CLI.
func TestMCPInputKindAddsUp(t *testing.T) {
	whole := newPagedDemo(t, 16<<20)
	var tl []struct {
		inputKindOut
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal([]byte(whole.cli("timeline", "--actor=all", "--since=2000-01-01", "--until=2100-01-01", "--limit=0")), &tl); err != nil {
		t.Fatal(err)
	}
	special := map[string]bool{}
	for _, e := range tl {
		if e.InputKind != nil && *e.InputKind != "typed" {
			special[e.SessionID] = true
		}
	}
	if len(special) == 0 {
		t.Fatal("no session with a pasted or remote input")
	}
	kinds := map[string]int{}
	human, unknown, maxPages := 0, 0, 0
	for id := range special {
		cliArgs := []string{"show", id, "--all", "--limit=1000"}
		want := decode[struct{ Events []inputKindOut }](t, whole.cli(cliArgs...))
		p := newPagedDemo(t, max(timelineBound(whole, 3, cliArgs...), 3<<10)) // the head of the session must fit
		pages, _ := p.crawl("show", map[string]any{"session": id, "all": true, "limit": 1000})
		n := 0
		for _, pg := range pages {
			for _, e := range decode[struct{ Events []inputKindOut }](t, pg).Events {
				n++
				switch {
				case e.Actor != "human" && e.InputKind != nil:
					t.Errorf("%s: %s event has input_kind %q", id, e.Actor, *e.InputKind)
				case e.Actor == "human" && e.InputKind == nil:
					human++
					unknown++
				case e.Actor == "human":
					human++
					kinds[*e.InputKind]++
				}
			}
		}
		maxPages = max(maxPages, len(pages))
		if n != len(want.Events) {
			t.Errorf("%s: %d pages hold %d events, CLI %d", id, len(pages), n, len(want.Events))
		}
	}
	if maxPages < 2 {
		t.Errorf("no session came in several pages")
	}
	sum := unknown
	for _, c := range kinds {
		sum += c
	}
	if sum != human || human == 0 || kinds["pasted"] == 0 || kinds["remote"] == 0 {
		t.Errorf("kinds %v + %d unknown = %d, human events %d", kinds, unknown, sum, human)
	}
}

type tev struct {
	Actor     string
	InputKind *string `json:"input_kind"`
}

func TestTimelineInputKind(t *testing.T) {
	w := []string{"--since", "2000-01-01", "--until", "2100-01-01"}
	count := func(evs []tev) (kinds map[string]int, human int) {
		kinds = map[string]int{}
		for _, e := range evs {
			switch {
			case e.Actor != "human":
				if e.InputKind != nil {
					t.Errorf("%s event has input_kind %q", e.Actor, *e.InputKind)
				}
			case e.InputKind == nil:
				human++
				kinds["unknown"]++
			default:
				human++
				kinds[*e.InputKind]++
			}
		}
		return kinds, human
	}
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now, e.home = demo.Now, demo.Home
	r := e.run(t, "", append([]string{"--db", db, "timeline", "--json", "--actor", "all", "--all", "--limit", "0"}, w...)...)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, `"input_kind"`) {
		t.Fatal("timeline --json has no input_kind key")
	}
	cli := decode[[]tev](t, r.stdout)
	kinds, human := count(cli)
	sum := 0
	for _, c := range kinds {
		sum += c
	}
	if human == 0 || sum != human || kinds["unknown"] != 0 || kinds["typed"] == 0 || kinds["pasted"] == 0 || kinds["remote"] == 0 {
		t.Errorf("kinds %v, %d human events", kinds, human)
	}

	// The MCP pages of a window with every actor and kind: about four
	// pages, so the number of CLI runs does not grow with the archive.
	const since, until = "2026-10-20", "2026-10-21"
	cliArgs := []string{"timeline", "--since=" + since, "--until=" + until, "--actor=all", "--all"}
	p := newPagedDemo(t, timelineBound(newPagedDemo(t, 16<<20), 4, cliArgs...))
	want := p.cli(cliArgs...)
	pages, _ := p.crawl("timeline", map[string]any{"since": since, "until": until, "actor": "all", "all": true})
	if len(pages) < 3 || len(pages) > 6 {
		t.Fatalf("%d pages for %d bytes", len(pages), len(want))
	}
	if got := reassemble(t, pages, ""); got != want {
		t.Errorf("%d pages do not add up to the CLI JSON (%d vs %d bytes)", len(pages), len(got), len(want))
	}
	var all, ref []tev
	for _, pg := range pages {
		all = append(all, decode[[]tev](t, pg)...)
	}
	ref = decode[[]tev](t, want)
	if len(all) != len(ref) {
		t.Fatalf("pages hold %d events, CLI %d", len(all), len(ref))
	}
	for _, pg := range pages {
		if !strings.Contains(pg, `"input_kind"`) {
			t.Fatal("a page has no input_kind key")
		}
	}
	if mk, mh := count(all); mh == 0 || func() int {
		n := 0
		for _, c := range mk {
			n += c
		}
		return n
	}() != mh {
		t.Errorf("MCP pages: kinds %v over %d human events", mk, mh)
	}
}
