package demo_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/screenshot"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

var (
	poolOnce sync.Once
	poolVal  demo.Pool
	poolErr  error
)

// sharedPool builds the demo pool once for the package.
func sharedPool(t *testing.T) demo.Pool {
	t.Helper()
	poolOnce.Do(func() {
		dir, err := os.MkdirTemp("", "total-recall-pool-")
		if err != nil {
			poolErr = err
			return
		}
		poolVal, poolErr = demo.BuildPool(ctx, dir)
	})
	if poolErr != nil {
		t.Fatalf("BuildPool: %v", poolErr)
	}
	return poolVal
}

// TestPoolSplitsTheDemo: the hosts of the pool hold disjoint parts of the
// demo transcripts that add up to the reference archive All, which holds
// the same sessions and events as the demo archive.
func TestPoolSplitsTheDemo(t *testing.T) {
	p := sharedPool(t)
	if len(p.Hosts) != len(demo.PoolHosts) {
		t.Fatalf("hosts = %d, want %d", len(p.Hosts), len(demo.PoolHosts))
	}
	_, shared := fixturedb.Demo(t)
	ref := rawDB(t, shared)
	all := rawDB(t, p.All)
	for _, q := range []string{
		"SELECT count(*) FROM sessions",
		"SELECT count(*) FROM events",
		"SELECT count(*) FROM events WHERE actor = 'human'",
		"SELECT count(*) FROM events WHERE kind = 'tool_use'",
	} {
		want := count(t, ref, q)
		if got := count(t, all, q); got != want {
			t.Errorf("All: %s = %d, demo archive %d", q, got, want)
		}
		sum := 0
		for _, h := range p.Hosts {
			n := count(t, rawDB(t, h.Path), q)
			if n == 0 {
				t.Errorf("%s: %s = 0", h.Host, q)
			}
			sum += n
		}
		if sum != want {
			t.Errorf("hosts: %s sums to %d, want %d", q, sum, want)
		}
	}
	for i, h := range p.Hosts {
		if h.Host != demo.PoolHosts[i] {
			t.Errorf("host %d = %q, want %q", i, h.Host, demo.PoolHosts[i])
		}
		db := rawDB(t, h.Path)
		if n := count(t, db, "SELECT count(*) FROM sessions"); n != h.Sessions {
			t.Errorf("%s: sessions = %d, summary %d", h.Host, n, h.Sessions)
		}
		if n := count(t, db, "SELECT count(*) FROM events"); n != h.Events {
			t.Errorf("%s: events = %d, summary %d", h.Host, n, h.Events)
		}
	}
	// No session is on two hosts.
	seen := map[string]string{}
	for _, h := range p.Hosts {
		for _, id := range column(t, rawDB(t, h.Path), "SELECT id FROM sessions") {
			if o, ok := seen[id]; ok {
				t.Errorf("session %s on %s and %s", id, o, h.Host)
			}
			seen[id] = h.Host
		}
	}
}

// TestPoolHosts: host-b is the agent box with sessions of the kompass
// worktree and wetterwarte only, host-a the notebook with every other
// directory; kompass is worked on from both, so its repository appears on
// both hosts.
func TestPoolHosts(t *testing.T) {
	p := sharedPool(t)
	cwds := map[string][]string{}
	for _, h := range p.Hosts {
		cwds[h.Host] = column(t, rawDB(t, h.Path), "SELECT DISTINCT cwd FROM sessions WHERE parent_id IS NULL ORDER BY cwd")
	}
	onB := map[string]bool{demo.Home + "/src/kompass__worktrees/gpx": true, demo.Home + "/src/wetterwarte": true}
	for _, c := range cwds["host-b"] {
		if !onB[c] {
			t.Errorf("host-b has a session in %s", c)
		}
	}
	if n := count(t, rawDB(t, p.Hosts[0].Path), "SELECT count(*) FROM sessions WHERE cwd = ?", demo.Home+"/src/wetterwarte"); n != 0 {
		t.Errorf("host-a has %d sessions of wetterwarte", n)
	}
	if got, want := len(cwds["host-a"]), 5; got != want {
		t.Errorf("host-a cwds = %v, want %d", cwds["host-a"], want)
	}
	if len(cwds["host-b"]) != len(onB) {
		t.Errorf("host-b cwds = %v", cwds["host-b"])
	}
	for _, h := range p.Hosts {
		if n := count(t, rawDB(t, h.Path), "SELECT count(*) FROM sessions WHERE git_root = ?", demo.Home+"/src/kompass"); n == 0 {
			t.Errorf("%s: no session of kompass", h.Host)
		}
	}
}

// TestPoolTokensAndModels: every agent event of the pool carries a model
// and the first event of each API message its tokens, split into fresh
// input, cache write and cache read; the main sessions switch models on
// demo.ModelSwitch, the subagents run on the small model. Tokens per
// model add up over the hosts to the reference archive.
func TestPoolTokensAndModels(t *testing.T) {
	p := sharedPool(t)
	all := rawDB(t, p.All)
	if n := count(t, all, "SELECT count(*) FROM events WHERE actor = 'agent' AND (model IS NULL OR model = '')"); n != 0 {
		t.Errorf("%d agent events without a model", n)
	}
	if n := count(t, all, "SELECT count(*) FROM events WHERE actor = 'agent' AND model = 'test-model'"); n != 0 {
		t.Errorf("%d agent events with the fixture model", n)
	}
	if n := count(t, all, "SELECT count(*) FROM events WHERE actor = 'agent' AND (tokens_in IS NULL OR tokens_out IS NULL OR tokens_out = 0)"); n != 0 {
		t.Errorf("%d agent events without tokens", n)
	}
	if n := count(t, all, "SELECT count(*) FROM events WHERE tokens_in IS NOT NULL AND (tokens_cache_read IS NULL OR tokens_cache_write IS NULL OR tokens_in < tokens_cache_read + tokens_cache_write)"); n != 0 {
		t.Errorf("%d events whose input does not split", n)
	}
	if n := count(t, all, "SELECT count(*) FROM events WHERE tokens_cache_read > 0"); n == 0 {
		t.Error("no cache reads")
	}
	models := column(t, all, "SELECT DISTINCT model FROM events WHERE model IS NOT NULL ORDER BY model")
	want := []string{demo.ModelLarge, demo.ModelNext, demo.ModelSmall}
	if len(models) != len(want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
	switchAt := demo.ModelSwitch.UTC().Format("2006-01-02T15:04:05")
	for _, tc := range []struct {
		name, q string
	}{
		{"large model after the switch", "SELECT count(*) FROM events e JOIN sessions s ON s.id = e.session_id WHERE s.parent_id IS NULL AND e.model = '" + demo.ModelLarge + "' AND e.ts >= '" + switchAt + "'"},
		{"next model before the switch", "SELECT count(*) FROM events e JOIN sessions s ON s.id = e.session_id WHERE s.parent_id IS NULL AND e.model = '" + demo.ModelNext + "' AND e.ts < '" + switchAt + "'"},
		{"subagent not on the small model", "SELECT count(*) FROM events e JOIN sessions s ON s.id = e.session_id WHERE s.parent_id IS NOT NULL AND e.actor = 'agent' AND e.model <> '" + demo.ModelSmall + "'"},
		{"main session on the small model", "SELECT count(*) FROM events e JOIN sessions s ON s.id = e.session_id WHERE s.parent_id IS NULL AND e.model = '" + demo.ModelSmall + "'"},
	} {
		if n := count(t, all, tc.q); n != 0 {
			t.Errorf("%s: %d events", tc.name, n)
		}
	}
	const byModel = "SELECT model, sum(tokens_in), sum(tokens_out), sum(tokens_cache_read), sum(tokens_cache_write) FROM events WHERE model IS NOT NULL GROUP BY model"
	want4 := tokenSums(t, all, byModel)
	got4 := map[string][4]int64{}
	for _, h := range p.Hosts {
		for m, v := range tokenSums(t, rawDB(t, h.Path), byModel) {
			g := got4[m]
			for i := range g {
				g[i] += v[i]
			}
			got4[m] = g
		}
	}
	for m, w := range want4 {
		if got4[m] != w {
			t.Errorf("%s: hosts %v, All %v", m, got4[m], w)
		}
	}
}

// TestPoolSynthetic: like the demo archive, nothing in the pool points at
// the machine that built it, and a second build yields the same events.
func TestPoolSynthetic(t *testing.T) {
	p := sharedPool(t)
	paths := []string{p.All}
	for _, h := range p.Hosts {
		paths = append(paths, h.Path)
	}
	for _, path := range paths {
		needles := []string{"/Users", "seletz", "/tmp", "/var/", os.TempDir(), filepath.Dir(path)}
		if hit, err := screenshot.Scan(ctx, rawDB(t, path), needles); err != nil {
			t.Fatal(err)
		} else if hit != "" {
			t.Errorf("%s holds a real path in %s", filepath.Base(path), hit)
		}
	}
	again, err := demo.BuildPool(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i, h := range again.Hosts {
		if dump(t, h.Path) != dump(t, p.Hosts[i].Path) {
			t.Errorf("%s: two builds differ", h.Host)
		}
	}
}

func column(t *testing.T, db *sql.DB, q string) []string {
	t.Helper()
	rows, err := db.Query(q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func tokenSums(t *testing.T, db *sql.DB, q string) map[string][4]int64 {
	t.Helper()
	rows, err := db.Query(q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][4]int64{}
	for rows.Next() {
		var m string
		var v [4]int64
		if err := rows.Scan(&m, &v[0], &v[1], &v[2], &v[3]); err != nil {
			t.Fatal(err)
		}
		out[m] = v
	}
	return out
}

// TestArchiveIdentity: the demo archive and the archives of the pool carry
// fixed IDs (a random one would change every build) and the host they
// belong to; all.db is no host's archive and names none.
func TestArchiveIdentity(t *testing.T) {
	p := sharedPool(t)
	_, shared := fixturedb.Demo(t)
	want := map[string][2]string{
		shared: {demo.ArchiveID, demo.Host},
		p.All:  {demo.PoolArchiveID("all"), ""},
	}
	for _, h := range p.Hosts {
		want[h.Path] = [2]string{demo.PoolArchiveID(h.Host), h.Host}
	}
	ids := map[string]bool{}
	for path, w := range want {
		db := rawDB(t, path)
		got := [2]string{}
		for i, k := range []string{"archive_id", "host"} {
			if err := db.QueryRow("SELECT coalesce((SELECT value FROM archive_meta WHERE key = ?), '')", k).Scan(&got[i]); err != nil {
				t.Fatal(err)
			}
		}
		if got != w {
			t.Errorf("%s: id, host = %v, want %v", filepath.Base(path), got, w)
		}
		if ids[got[0]] {
			t.Errorf("%s: ID %s used twice", filepath.Base(path), got[0])
		}
		ids[got[0]] = true
	}
}
