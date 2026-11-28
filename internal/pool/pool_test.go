package pool

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/store"
)

var ctx = context.Background()

var berlin = func() *time.Location {
	l, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		panic(err)
	}
	return l
}()

// backup writes a real backup of a small archive of host into dir at now
// and returns its path.
func backup(t *testing.T, dir, host string, now time.Time) string {
	t.Helper()
	db := filepath.Join(t.TempDir(), "archive.db")
	st, err := store.Open(db, store.WithHost(host))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	p, err := st.Backup(ctx, dir, now, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// TestPublish: the newest backup goes into <pool>/<host>/ under its own
// name with its time kept; a second run with nothing new copies nothing;
// a newer backup replaces the older copy, so the pool holds one per host;
// files that are no backup stay.
func TestPublish(t *testing.T) {
	local, pool := t.TempDir(), t.TempDir()
	day1 := time.Date(2026, 11, 2, 18, 0, 0, 0, berlin)
	b1 := backup(t, local, "host-a", day1)
	if err := os.MkdirAll(filepath.Join(pool, "host-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pool, "host-a", "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Publish(local, pool, "host-a", berlin)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(pool, "host-a", filepath.Base(b1))
	if r.Path != want || !r.Copied || len(r.Removed) != 0 {
		t.Fatalf("first publish = %+v, want a copy at %s", r, want)
	}
	if p, at, _ := store.LastBackup(filepath.Join(pool, "host-a"), berlin); p != want || !at.Equal(lastAt(t, local)) {
		t.Errorf("pool newest = %s at %s, want %s at the time of the local copy", p, at, want)
	}
	if r, err := Publish(local, pool, "host-a", berlin); err != nil || r.Copied || r.Path != want {
		t.Errorf("second publish = %+v (%v), want no copy", r, err)
	}

	b2 := backup(t, local, "host-a", day1.Add(24*time.Hour))
	r, err = Publish(local, pool, "host-a", berlin)
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != filepath.Join(pool, "host-a", filepath.Base(b2)) || !r.Copied || !slices.Equal(r.Removed, []string{want}) {
		t.Errorf("newer publish = %+v", r)
	}
	if got := names(t, filepath.Join(pool, "host-a")); !slices.Equal(got, []string{"README", filepath.Base(b2)}) {
		t.Errorf("pool/host-a = %v", got)
	}
}

func lastAt(t *testing.T, dir string) time.Time {
	t.Helper()
	_, at, err := store.LastBackup(dir, berlin)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// TestPublishErrors: nothing to publish without a local backup or a host,
// and a host that is no valid folder name is refused.
func TestPublishErrors(t *testing.T) {
	local, pool := t.TempDir(), t.TempDir()
	for _, tc := range []struct {
		name, host, want string
		backup           bool
	}{
		{"no backup", "host-a", "no backup", false},
		{"no host", "", "host", true},
		{"bad host", "../x", "host", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(local, tc.name)
			if tc.backup {
				backup(t, dir, "host-a", time.Date(2026, 11, 2, 18, 0, 0, 0, berlin))
			}
			if _, err := Publish(dir, pool, tc.host, berlin); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one about %q", err, tc.want)
			}
		})
	}
	if got := names(t, pool); len(got) != 0 {
		t.Errorf("pool written: %v", got)
	}
}

// TestScan: every host folder with a backup is an entry with its newest
// copy; folders without one, files, temporary copies and names that are
// no host are left out.
func TestScan(t *testing.T) {
	pool := t.TempDir()
	day := time.Date(2026, 11, 2, 18, 0, 0, 0, berlin)
	backup(t, filepath.Join(pool, "host-a"), "host-a", day)
	newest := backup(t, filepath.Join(pool, "host-a"), "host-a", day.Add(time.Hour))
	b := backup(t, filepath.Join(pool, "host-b"), "host-b", day)
	for _, d := range []string{"empty", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(pool, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	backup(t, filepath.Join(pool, ".hidden"), "x", day)
	if err := os.WriteFile(filepath.Join(pool, "host-b", "archive-x.tmp"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pool, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Scan(pool, berlin)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Host != "host-a" || got[0].Path != newest || got[1].Host != "host-b" || got[1].Path != b {
		t.Fatalf("Scan = %+v", got)
	}
	if got[0].Size <= 0 || got[0].At.IsZero() {
		t.Errorf("entry without size or time: %+v", got[0])
	}
	if got, err := Scan(filepath.Join(pool, "missing"), berlin); err == nil || got != nil {
		t.Errorf("Scan of a missing pool = %v, %v; want an error", got, err)
	}
}

// TestOpen: a pool copy is unpacked once into the cache and read with
// immutable=1, without writing next to it; an unchanged copy reuses the
// cache, a newer one replaces it. The pool directory is never written.
func TestOpen(t *testing.T) {
	pool, cache := t.TempDir(), t.TempDir()
	day := time.Date(2026, 11, 2, 18, 0, 0, 0, berlin)
	backup(t, filepath.Join(pool, "host-a"), "host-a", day)
	if err := os.Chmod(filepath.Join(pool, "host-a"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(pool, "host-a"), 0o755) })
	entries, err := Scan(pool, berlin)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Open(ctx, entries[0], cache)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a.Path, filepath.Join(cache, "host-a")+string(filepath.Separator)) || !a.Unpacked {
		t.Fatalf("Open = %+v", a)
	}
	if a.Archive.Host != "host-a" || len(a.Archive.ID) != 32 || a.SchemaVersion != store.LatestSchemaVersion() {
		t.Errorf("identity = %+v, schema %d", a.Archive, a.SchemaVersion)
	}
	for _, sfx := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(a.Path + sfx); err == nil {
			t.Errorf("%s written next to the cached copy", sfx)
		}
	}
	if !strings.HasPrefix(a.URI(), "file:") || !strings.Contains(a.URI(), "immutable=1") || !strings.Contains(a.URI(), "mode=ro") {
		t.Errorf("URI = %s", a.URI())
	}
	again, err := Open(ctx, entries[0], cache)
	if err != nil || again.Unpacked || again.Path != a.Path {
		t.Errorf("second Open = %+v (%v), want the cached copy", again, err)
	}
	if err := os.Chmod(filepath.Join(pool, "host-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	backup(t, filepath.Join(pool, "host-a"), "host-a", day.Add(24*time.Hour))
	entries, err = Scan(pool, berlin)
	if err != nil {
		t.Fatal(err)
	}
	newer, err := Open(ctx, entries[0], cache)
	if err != nil || !newer.Unpacked || newer.Path == a.Path {
		t.Fatalf("Open of a newer copy = %+v (%v)", newer, err)
	}
	if _, err := os.Stat(a.Path); err == nil {
		t.Errorf("old cached copy %s left behind", a.Path)
	}
}
