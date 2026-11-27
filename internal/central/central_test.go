package central

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/harness/claude"
	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/store"
)

var ctx = context.Background()

func openStore(t *testing.T, p string) *store.Store {
	t.Helper()
	s, err := store.Open(p, store.WithLocation(demo.Location))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// count runs a query that returns one number; "?" is replaced by arg
// (a host name of the test, no quoting needed).
func count(t *testing.T, s *store.Store, q string, arg ...string) int {
	t.Helper()
	if len(arg) > 0 {
		q = strings.Replace(q, "?", "'"+arg[0]+"'", 1)
	}
	_, rows, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return int(rows[0][0].(int64))
}

// TestPoolSumEqualsParts splits the demo archive over two hosts, pushes
// both over TLS into a central archive and checks that the central archive
// holds exactly the reference archive, and per host exactly the host's
// local archive.
func TestPoolSumEqualsParts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the demo archive three times")
	}
	dir := t.TempDir()
	hosts := []string{"host-a", "host-b"}
	pool, err := demo.BuildPool(ctx, dir, hosts)
	if err != nil {
		t.Fatal(err)
	}
	central := openStore(t, filepath.Join(dir, "central.db"))
	srv := &Server{Store: central, Hosts: map[string]string{HashToken("token-a"): "host-a", HashToken("token-b"): "host-b"}}
	ts := httptest.NewTLSServer(srv.Handler())
	defer ts.Close()

	tokens := map[string]string{"host-a": "token-a", "host-b": "token-b"}
	for _, h := range hosts {
		local := openStore(t, pool.Hosts[h])
		c := &Client{URL: ts.URL, Token: tokens[h], HTTP: ts.Client()}
		sum, err := Push(ctx, local, c, StatePath(pool.Hosts[h], ts.URL), 2000)
		if err != nil {
			t.Fatalf("push %s: %v", h, err)
		}
		if sum.Batches < 2 || sum.Received.Conflicts != 0 {
			t.Errorf("push %s: %+v", h, sum)
		}
		// A second push has nothing to deliver: one empty request (heartbeat).
		again, err := Push(ctx, local, c, StatePath(pool.Hosts[h], ts.URL), 2000)
		if err != nil || again.Batches != 1 || again.Received != (store.ImportResult{}) {
			t.Errorf("second push %s: %+v, %v", h, again, err)
		}
	}

	all := openStore(t, pool.All)
	loc := demo.Location
	stats := func(s *store.Store) store.Stats {
		t.Helper()
		st, err := s.Stats(ctx, store.StatsFilter{Location: loc})
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	// Sum equals parts: the central totals are the hosts' totals.
	var parts store.Activity
	for _, h := range hosts {
		tot := stats(openStore(t, pool.Hosts[h])).Total
		parts.Sessions += tot.Sessions
		parts.Human += tot.Human
		parts.Agent += tot.Agent
	}
	if got := stats(central).Total; got != parts {
		t.Errorf("central stats %+v, host-a + host-b %+v", got, parts)
	}
	// Against the reference (all transcripts on one machine) two human
	// inputs differ: a coordinator session on host-b started worker
	// sessions on host-a (#269); each host alone cannot see that, so on
	// host-a the first prompts count as human. Reclassifying the central
	// archive, which sees both, makes it equal to the reference.
	if got, want := stats(central).Total.Human, stats(all).Total.Human+2; got != want {
		t.Errorf("central human events before reindex = %d, want reference + 2 = %d", got, want)
	}
	rs, err := ingest.Reclassify(ctx, ingest.Deps{Store: central, Adapters: []harness.Adapter{claude.New(filepath.Join(dir, "none"))}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if rs.Transitions["human -> automation"] != 2 {
		t.Errorf("reclassify transitions = %v, want 2 human -> automation", rs.Transitions)
	}
	want, got := stats(all), stats(central)
	if !reflect.DeepEqual(got.Total, want.Total) || !reflect.DeepEqual(got.ByDay, want.ByDay) || !reflect.DeepEqual(got.ByRepo, want.ByRepo) {
		t.Errorf("central stats after reindex %+v, reference %+v", got.Total, want.Total)
	}
	for _, q := range []string{
		"SELECT count(*) FROM sessions",
		"SELECT count(*) FROM events",
		"SELECT count(*) FROM session_meta",
		"SELECT count(*) FROM blobs",
		"SELECT count(*) FROM events_fts WHERE events_fts MATCH 'goreleaser AND cask'",
		"SELECT COALESCE(sum(tokens_in), 0) + COALESCE(sum(tokens_out), 0) FROM events",
	} {
		if c, r := count(t, central, q), count(t, all, q); c != r {
			t.Errorf("%s: central %d, reference %d", q, c, r)
		}
	}
	hits, err := central.Search(ctx, demo.Query, store.SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := all.Search(ctx, demo.Query, store.SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != len(ref) || len(hits) == 0 {
		t.Errorf("search %q: central %d hits, reference %d", demo.Query, len(hits), len(ref))
	}

	// Per host: the central rows of a host are the host's local archive.
	sumSessions, sumEvents := 0, 0
	for _, h := range hosts {
		local := openStore(t, pool.Hosts[h])
		ls, le := count(t, local, "SELECT count(*) FROM sessions"), count(t, local, "SELECT count(*) FROM events")
		cs := count(t, central, "SELECT count(*) FROM sessions WHERE host = ?", h)
		ce := count(t, central, "SELECT count(*) FROM events e JOIN sessions s ON s.id = e.session_id WHERE s.host = ?", h)
		if ls != cs || le != ce || ls == 0 {
			t.Errorf("%s: local %d sessions/%d events, central %d/%d", h, ls, le, cs, ce)
		}
		t.Logf("%s: %d projects, %d sessions, %d events", h, len(pool.Projects[h]), ls, le)
		sumSessions += ls
		sumEvents += le
	}
	if r := count(t, all, "SELECT count(*) FROM sessions"); sumSessions != r {
		t.Errorf("sessions host-a + host-b = %d, reference %d", sumSessions, r)
	}
	if r := count(t, all, "SELECT count(*) FROM events"); sumEvents != r {
		t.Errorf("events host-a + host-b = %d, reference %d", sumEvents, r)
	}
}

func TestServerRefusesUnknownToken(t *testing.T) {
	central := openStore(t, filepath.Join(t.TempDir(), "central.db"))
	srv := &Server{Store: central, Hosts: map[string]string{HashToken("token-a"): "host-a"}}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := &Client{URL: ts.URL, Token: "wrong"}
	if _, err := c.Send(ctx, store.Changeset{}); err == nil {
		t.Error("unknown token accepted")
	}
}

// TestPushOffline: an unreachable receiver leaves the state untouched; the
// next push delivers everything.
func TestPushOffline(t *testing.T) {
	dir := t.TempDir()
	pool, err := demo.BuildPool(ctx, dir, []string{"host-a"})
	if err != nil {
		t.Fatal(err)
	}
	local := openStore(t, pool.Hosts["host-a"])
	state := filepath.Join(dir, "push.json")
	down := &Client{URL: "http://127.0.0.1:1", Token: "token-a", HTTP: &http.Client{Timeout: time.Second}}
	if _, err := Push(ctx, local, down, state, 500); err == nil {
		t.Fatal("push to a closed port: no error")
	}
	if st, err := LoadState(state); err != nil || st.Events != 0 {
		t.Errorf("state after failed push = %+v, %v", st, err)
	}

	central := openStore(t, filepath.Join(dir, "central.db"))
	srv := &Server{Store: central, Hosts: map[string]string{HashToken("token-a"): "host-a"}}
	// The receiver fails after the first batch: the acknowledged batch
	// stays acknowledged, the rest comes with the next push.
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			http.Error(w, "boom", http.StatusServiceUnavailable)
			return
		}
		srv.Handler().ServeHTTP(w, r)
	}))
	defer ts.Close()
	up := &Client{URL: ts.URL, Token: "token-a"}
	if _, err := Push(ctx, local, up, state, 500); err == nil {
		t.Fatal("failing receiver: no error")
	}
	if _, err := Push(ctx, local, up, state, 500); err != nil {
		t.Fatal(err)
	}
	if c, l := count(t, central, "SELECT count(*) FROM events"), count(t, local, "SELECT count(*) FROM events"); c != l {
		t.Errorf("events central %d, local %d", c, l)
	}
}
