package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	trcli "github.com/seletz/total-recall/internal/cli"
	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/store"
)

// poolEnv is the demo pool laid out as a pool directory (one backup per
// host in <pool>/<host>/) with a config whose archive is the pool's
// reference all.db, so stats on it is the whole pool.
type poolEnv struct {
	*env
	pool, cache string
	hosts       map[string]string // host -> its archive
}

func newPoolEnv(t *testing.T) *poolEnv {
	t.Helper()
	dir := t.TempDir()
	p, err := demo.BuildPool(context.Background(), filepath.Join(dir, "src"))
	if err != nil {
		t.Fatal(err)
	}
	pe := &poolEnv{pool: filepath.Join(dir, "pool"), cache: filepath.Join(dir, "cache"), hosts: map[string]string{}}
	at := time.Date(2026, 11, 2, 3, 0, 0, 0, demo.Location)
	for _, h := range p.Hosts {
		st, err := store.Open(h.Path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.Backup(context.Background(), filepath.Join(pe.pool, h.Host), at, 7, 0); err != nil {
			t.Fatal(err)
		}
		_ = st.Close()
		pe.hosts[h.Host] = h.Path
	}
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, fmt.Appendf(nil, "db = %q\ntimezone = \"Europe/Berlin\"\n\n[backup]\npool_dir = %q\n\n[git]\nresolve = false\n", p.All, pe.pool), 0o644); err != nil {
		t.Fatal(err)
	}
	// The folder of the analytics database does not exist yet, as on a
	// machine that only analyses the pool.
	pe.env = &env{t: t, archive: p.All, cfg: cfg, analytics: filepath.Join(dir, "new", "analytics.duckdb")}
	return pe
}

// statsOf runs `total-recall stats --json` on archive.
func (e *env) statsOf(archive string, args ...string) map[string]any {
	e.t.Helper()
	var out, errb bytes.Buffer
	code := trcli.Main(&trcli.App{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb, Home: demo.Home, Getenv: e.getenv,
		Args: append([]string{"--db", archive, "stats", "--since", "2026-05-01", "--until", "2026-11-30", "--json"}, args...),
		Now:  func() time.Time { return time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC) },
	})
	if code != 0 {
		e.t.Fatalf("stats %v: exit %d: %s", args, code, errb.String())
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

// TestBuildPool: build --pool loads every host of the pool directory, each
// as its own source under the host its archive names. Sum equals parts:
// per host the numbers equal stats --json of that host's archive, over
// the pool those of the reference archive with every host, work time
// included (it merges the hosts before gaps are credited).
func TestBuildPool(t *testing.T) {
	e := newPoolEnv(t)
	var b BuildPoolJSON
	e.json(&b, "build", "--db", e.analytics, "--pool", "--cache", e.cache)
	if b.Pool != e.pool || len(b.Builds) != 2 || len(b.Skipped) != 0 || b.Builds[0].Host != "host-a" || b.Builds[1].Host != "host-b" ||
		b.Builds[0].Mode != "full" || b.Builds[0].ArchiveID == nil || *b.Builds[0].ArchiveID != demo.PoolArchiveID("host-a") || !strings.HasPrefix(b.Builds[0].Archive, e.cache) {
		t.Fatalf("build --pool = %+v", b)
	}

	var byHost []UsageJSON
	e.json(&byHost, "usage", "--db", e.analytics, "--by", "host")
	var tokByHost []TokensJSON
	e.json(&tokByHost, "tokens", "--db", e.analytics, "--by", "host")
	if len(byHost) != 2 || len(tokByHost) != 2 {
		t.Fatalf("usage --by host = %+v, tokens %+v", byHost, tokByHost)
	}
	for i, u := range byHost {
		host := keyText(u.Key)
		st := e.statsOf(e.hosts[host])
		tt, tok := st["total"].(map[string]any), st["tokens"].(map[string]any)
		if u.Sessions != num(tt["sessions"]) || u.Human != num(tt["human"]) || u.Agent != num(tt["agent"]) {
			t.Errorf("%s: usage %+v, stats %v", host, u, tt)
		}
		k := tokByHost[i]
		if keyText(k.Key) != host || k.Events != num(tok["events"]) || k.Input != num(tok["input"]) || k.CacheRead != num(tok["cache_read"]) ||
			k.CacheWrite != num(tok["cache_write"]) || k.Fresh != num(tok["fresh"]) || k.Output != num(tok["output"]) {
			t.Errorf("%s: tokens %+v, stats %v", host, k, tok)
		}
	}

	// --host keeps one host: the same numbers as its row of --by host.
	for _, u := range byHost {
		var one []UsageJSON
		e.json(&one, "usage", "--db", e.analytics, "--by", "none", "--host", keyText(u.Key))
		if len(one) != 1 || one[0].Sessions != u.Sessions || one[0].Human != u.Human || one[0].ToolCalls != u.ToolCalls || one[0].TokensIn != u.TokensIn {
			t.Errorf("--host %s = %+v, --by host %+v", keyText(u.Key), one, u)
		}
	}
	var wh []WorktimeJSON
	e.json(&wh, "worktime", "--db", e.analytics, "--by", "category", "--host", "host-a")
	for _, w := range wh {
		if w.RoundedSeconds != 0 {
			t.Errorf("worktime --host has a booked time: %+v", w)
		}
	}

	all := e.statsOf(e.archive)
	var total []UsageJSON
	e.json(&total, "usage", "--db", e.analytics, "--by", "none")
	tt := all["total"].(map[string]any)
	if total[0].Sessions != num(tt["sessions"]) || total[0].Human != num(tt["human"]) || total[0].Agent != num(tt["agent"]) {
		t.Errorf("pool usage %+v, stats of all.db %v", total[0], tt)
	}
	wt := e.statsOf(e.archive, "--worktime")["total"].(map[string]any)
	var w []WorktimeJSON
	e.json(&w, "worktime", "--db", e.analytics, "--by", "none")
	if w[0].RawSeconds != num(wt["raw_seconds"]) || w[0].RoundedSeconds != num(wt["rounded_seconds"]) || w[0].Days != num(wt["days"]) {
		t.Errorf("pool worktime %+v, stats --worktime of all.db %v", w[0], wt)
	}
	before := e.mustRun("query", "--db", e.analytics, "--format", "csv", "SELECT host, count(*) FROM analytics_events_v1 GROUP BY host ORDER BY host")

	// A second folder with a copy of host-b's backup, and a second run:
	// the copy is skipped, nothing is loaded twice.
	src, _ := filepath.Glob(filepath.Join(e.pool, "host-b", "archive-*.db.zst"))
	cp := filepath.Join(e.pool, "host-c", filepath.Base(src[0]))
	data, err := os.ReadFile(src[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cp, data, 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(src[0])
	if err := os.Chtimes(cp, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	e.json(&b, "build", "--db", e.analytics, "--pool", "--cache", e.cache)
	if len(b.Builds) != 2 || len(b.Skipped) != 1 || b.Skipped[0].Host != "host-c" || !strings.Contains(b.Skipped[0].Reason, "copy of the archive of host-b") ||
		b.Builds[1].Mode != "incremental" || b.Builds[1].Events != 0 {
		t.Errorf("second build = %+v", b)
	}
	if after := e.mustRun("query", "--db", e.analytics, "--format", "csv", "SELECT host, count(*) FROM analytics_events_v1 GROUP BY host ORDER BY host"); after != before {
		t.Errorf("events per host changed:\n%s\nvs\n%s", before, after)
	}
	out := e.mustRun("build", "--db", e.analytics, "--pool", "--cache", e.cache)
	for _, want := range []string{"host-a", "host-b", "skipped host-c: another copy of the archive of host-b"} {
		if !strings.Contains(out, want) {
			t.Errorf("build --pool text lacks %q:\n%s", want, out)
		}
	}
	keys(t, e.mustRun("build", "--db", e.analytics, "--pool", "--cache", e.cache, "--json"), "analytics_db", "pool", "cache", "builds", "skipped")
	var st StatusJSON
	e.json(&st, "status", "--db", e.analytics)
	if len(st.Sources) != 2 || st.Sources[1].Source != "host-b" || st.Sources[1].ArchiveID == nil || *st.Sources[1].ArchiveID != demo.PoolArchiveID("host-b") || !st.Sources[1].ArchiveFound {
		t.Errorf("status sources = %+v", st.Sources)
	}
}

// TestBuildPoolErrors: --pool without a pool directory, or with flags that
// name one archive, is a usage error.
func TestBuildPoolErrors(t *testing.T) {
	e := newEnv(t)
	for _, args := range [][]string{
		{"build", "--db", e.analytics, "--pool", "--json"},
		{"build", "--db", e.analytics, "--pool-dir", "/nowhere", "--host", "x", "--json"},
	} {
		code, out, _ := e.run(args...)
		if code == 0 || !strings.Contains(out, `"kind":"usage"`) {
			t.Errorf("%v: exit %d, %s", args, code, out)
		}
	}
}
