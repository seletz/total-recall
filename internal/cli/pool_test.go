package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setPoolDir puts pool_dir into the [backup] table of the test config.
func setPoolDir(t *testing.T, e *testEnv, dir string) {
	t.Helper()
	b, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Replace(string(b), "[backup]\n", "[backup]\npool_dir = \""+dir+"\"\n", 1)
	if err := os.WriteFile(e.configPath, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBackupPool: with [backup] pool_dir, backup puts the newest copy into
// <pool>/<host>/; a skipped backup still publishes a copy the pool lacks,
// and says when the pool has it already.
func TestBackupPool(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	pool := filepath.Join(e.dir, "pool")
	setPoolDir(t, e, pool)
	e.mustRun(t, "ingest", "--all")
	out := e.mustRun(t, "backup")
	local, _ := filepath.Glob(filepath.Join(e.backups, "archive-*.db.zst"))
	if len(local) != 1 {
		t.Fatalf("local backups = %v", local)
	}
	copyPath := filepath.Join(pool, "host-a", filepath.Base(local[0]))
	if !strings.Contains(out, "backup written: "+local[0]) || !strings.Contains(out, "pool copy: "+copyPath+"\n") {
		t.Errorf("backup output:\n%s", out)
	}
	if _, err := os.Stat(copyPath); err != nil {
		t.Fatal(err)
	}
	out = e.mustRun(t, "backup", "--if-older", "24h")
	if !strings.Contains(out, "backup skipped") || !strings.Contains(out, "pool copy: "+copyPath+" (unchanged)") {
		t.Errorf("skipped backup output:\n%s", out)
	}
	if err := os.Remove(copyPath); err != nil {
		t.Fatal(err)
	}
	out = e.mustRun(t, "backup", "--if-older", "24h")
	if !strings.Contains(out, "pool copy: "+copyPath+"\n") {
		t.Errorf("catch-up output:\n%s", out)
	}
	s := decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
	if s.PoolDir != NullString(pool) || s.PoolCopy != NullString(copyPath) || s.PoolCopyAt == "" {
		t.Errorf("status pool = %q %q %q", s.PoolDir, s.PoolCopy, s.PoolCopyAt)
	}
	if out := e.mustRun(t, "status"); !strings.Contains(out, "pool                  "+copyPath) {
		t.Errorf("status lacks the pool line:\n%s", out)
	}
}

// TestBackupPoolWithoutHost: without a valid host the local backup is
// written, but the pool copy fails with a hint.
func TestBackupPoolWithoutHost(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	setPoolDir(t, e, filepath.Join(e.dir, "pool"))
	e.hostname = "bad name"
	e.mustRun(t, "ingest", "--all")
	r := e.run(t, "", "backup")
	if r.code == 0 || !strings.Contains(r.stdout, "backup written") || !strings.Contains(r.stderr, "set host in the config") {
		t.Errorf("backup = %+v", r)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "pool")); err == nil {
		t.Error("pool written without a host")
	}
}

// TestPoolCommand: pool lists the newest copy of every host with what the
// archive says about itself, and names what does not fit: a folder whose
// archive names another host, and two folders with the same archive.
func TestPoolCommand(t *testing.T) {
	t.Parallel()
	pool := t.TempDir()
	for _, h := range []string{"host-a", "host-b"} {
		e := newTestEnv(t)
		e.hostname = h
		setPoolDir(t, e, pool)
		e.mustRun(t, "ingest", "--all")
		e.mustRun(t, "backup")
	}
	// host-c holds a copy of host-b's archive: same ID, other folder.
	src, _ := filepath.Glob(filepath.Join(pool, "host-b", "archive-*.db.zst"))
	if err := os.MkdirAll(filepath.Join(pool, "host-c"), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(src[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pool, "host-c", filepath.Base(src[0])), b, 0o644); err != nil {
		t.Fatal(err)
	}
	e := newTestEnv(t)
	cache := filepath.Join(e.dir, "cache")
	p := decode[PoolJSON](t, e.mustRun(t, "pool", "--dir", pool, "--cache", cache, "--json"))
	if p.Pool != pool || p.Cache != cache || len(p.Hosts) != 3 {
		t.Fatalf("pool = %+v", p)
	}
	for i, want := range []struct {
		host, archiveHost string
		problems          int
	}{{"host-a", "host-a", 0}, {"host-b", "host-b", 1}, {"host-c", "host-b", 2}} {
		h := p.Hosts[i]
		if h.Host != want.host || string(h.ArchiveHost) != want.archiveHost || len(h.ArchiveID) != 32 || h.Sessions != 4 || len(h.Problems) != want.problems {
			t.Errorf("host %d = %+v", i, h)
		}
		if h.Backup == "" || h.BackupAt == "" || h.BackupBytes <= 0 || !strings.HasPrefix(h.Cached, cache) {
			t.Errorf("host %d copy = %+v", i, h)
		}
	}
	if !strings.Contains(strings.Join(p.Hosts[2].Problems, "|"), "archive names host host-b") ||
		!strings.Contains(strings.Join(p.Hosts[2].Problems, "|"), "same archive as host-b") {
		t.Errorf("host-c problems = %v", p.Hosts[2].Problems)
	}
	out := e.mustRun(t, "pool", "--dir", pool, "--cache", cache)
	for _, want := range []string{"HOST", "ARCHIVE", "host-a", "same archive as host-b"} {
		if !strings.Contains(out, want) {
			t.Errorf("pool text lacks %q:\n%s", want, out)
		}
	}
}

// TestPoolCommandErrors: without a pool directory pool is a usage error.
func TestPoolCommandErrors(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	r := e.run(t, "", "pool", "--json")
	if r.code == 0 || !strings.Contains(r.stdout+r.stderr, "pool_dir") {
		t.Errorf("pool without a directory = %+v", r)
	}
}
