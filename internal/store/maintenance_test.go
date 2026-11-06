package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/model"
)

// fillStore writes n events of varying size (so that rows span overflow
// pages) and returns their ids.
func fillStore(t *testing.T, s *Store, prefix string, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	var events []model.Event
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("claude:%s-%d", prefix, i)
		text := strings.Repeat("abcdefghij", 50+(i*37)%400) // 0.5 to 4.5 KiB
		e := ev(id, "claude:"+prefix, "2026-11-01T10:00:00Z", model.ActorAgent, model.KindMessage, "assistant", text)
		e.Raw = `{"i":` + fmt.Sprint(i) + `,"t":"` + text + `"}`
		events = append(events, e)
		ids = append(ids, id)
	}
	src := SourceState{Path: "/src/" + prefix + ".jsonl", Harness: "claude", Offset: int64(n)}
	if err := s.WriteBatch(ctx, src, []model.Session{{ID: "claude:" + prefix, Harness: "claude"}}, events); err != nil {
		t.Fatal(err)
	}
	return ids
}

// fingerprint returns a digest of every event row, so that two archives can
// be compared for identical content.
func fingerprint(t *testing.T, path string) string {
	t.Helper()
	s, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var fp string
	q := `SELECT count(*) || ':' || coalesce(sum(length(text)), 0) || ':' || coalesce(sum(length(raw)), 0) || ':' || coalesce(min(id), '') || ':' || coalesce(max(id), '') FROM events`
	if err := s.db().QueryRowContext(ctx, q).Scan(&fp); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db().QueryRowContext(ctx, `SELECT count(*) FROM events_fts WHERE events_fts MATCH 'abcdefghij'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%s fts=%d", fp, n)
}

func pageStats(t *testing.T, path string) PageStats {
	t.Helper()
	s, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ps, err := s.PageStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func maintainOpts(dir string) MaintainOptions {
	return MaintainOptions{
		PageSize:  16384,
		LockDir:   filepath.Join(dir, "locks"),
		BackupDir: filepath.Join(dir, "backups"),
		Now:       ts("2026-11-04T08:00:00Z"),
		KeepDaily: 7,
	}
}

func TestMaintainChangesPageSize(t *testing.T) {
	s, p := openTestStore(t)
	fillStore(t, s, "a", 300)
	_ = s.Close()
	dir := filepath.Dir(p)
	before := fingerprint(t, p)
	if ps := pageStats(t, p); ps.PageSize != 4096 || ps.Events != 300 || ps.Sessions != 1 || ps.Sources != 1 || ps.SizeBytes == 0 || ps.PageCount == 0 {
		t.Fatalf("stats before = %+v", ps)
	}

	res, err := Maintain(ctx, p, maintainOpts(dir))
	if err != nil {
		t.Fatalf("Maintain: %v", err)
	}
	if res.Before.PageSize != 4096 || res.After.PageSize != 16384 || !res.Swapped || res.DryRun {
		t.Errorf("result = %+v", res)
	}
	if res.Before.Events != 300 || res.After.Events != 300 || res.After.Sessions != 1 || res.After.Sources != 1 {
		t.Errorf("counts = %+v / %+v", res.Before, res.After)
	}
	if res.Backup == "" {
		t.Error("no backup written")
	} else if _, err := os.Stat(res.Backup); err != nil {
		t.Error(err)
	}
	if got := fingerprint(t, p); got != before {
		t.Errorf("content changed: %s -> %s", before, got)
	}
	if ps := pageStats(t, p); ps.PageSize != 16384 || ps.FreelistPages != 0 {
		t.Errorf("stats after = %+v", ps)
	}
	// No temporary files are left next to the database, and the archive
	// opens read-write (WAL mode) again.
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), "maint") {
			t.Errorf("leftover %s", e.Name())
		}
	}
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	var jm string
	if err := s2.db().QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&jm); err != nil || jm != "wal" {
		t.Errorf("journal_mode = %q, %v", jm, err)
	}
	fillStore(t, s2, "b", 5)
	_ = s2.Close()
}

func TestMaintainSamePageSizeOnlyVacuums(t *testing.T) {
	s, p := openTestStore(t)
	fillStore(t, s, "a", 50)
	_ = s.Close()
	o := maintainOpts(filepath.Dir(p))
	o.PageSize = 4096
	res, err := Maintain(ctx, p, o)
	if err != nil {
		t.Fatal(err)
	}
	if res.After.PageSize != 4096 || !res.Swapped {
		t.Errorf("result = %+v", res)
	}
}

func TestMaintainRejectsBadPageSize(t *testing.T) {
	_, p := openTestStore(t)
	for _, ps := range []int{0, 100, 3000, 131072} {
		o := maintainOpts(filepath.Dir(p))
		o.PageSize = ps
		if _, err := Maintain(ctx, p, o); err == nil {
			t.Errorf("page size %d accepted", ps)
		}
	}
}

func TestMaintainDryRunLeavesArchiveAlone(t *testing.T) {
	s, p := openTestStore(t)
	fillStore(t, s, "a", 200)
	_ = s.Close()
	dir := filepath.Dir(p)
	fi, _ := os.Stat(p)
	before := fingerprint(t, p)

	o := maintainOpts(dir)
	o.DryRun = true
	res, err := Maintain(ctx, p, o)
	if err != nil {
		t.Fatalf("Maintain: %v", err)
	}
	if !res.DryRun || res.Swapped || res.Backup != "" {
		t.Errorf("result = %+v", res)
	}
	if res.Before.PageSize != 4096 || res.After.PageSize != 16384 || res.After.SizeBytes == 0 || res.After.Events != 200 {
		t.Errorf("stats = %+v / %+v", res.Before, res.After)
	}
	if fi2, _ := os.Stat(p); fi2.Size() != fi.Size() || !fi2.ModTime().Equal(fi.ModTime()) {
		t.Error("dry run touched the archive")
	}
	if pageStats(t, p).PageSize != 4096 || fingerprint(t, p) != before {
		t.Error("dry run changed the archive")
	}
	if _, err := os.Stat(o.BackupDir); !errors.Is(err, os.ErrNotExist) {
		t.Error("dry run wrote a backup")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), "maint") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}

func TestMaintainAbortKeepsOriginal(t *testing.T) {
	s, p := openTestStore(t)
	fillStore(t, s, "a", 100)
	_ = s.Close()
	dir := filepath.Dir(p)
	before := fingerprint(t, p)

	// The copy fails its check: nothing is swapped, no files are left over.
	orig := checkCopy
	checkCopy = func(ctx context.Context, path string, want PageStats) (PageStats, error) {
		return PageStats{}, errors.New("boom")
	}
	_, err := Maintain(ctx, p, maintainOpts(dir))
	checkCopy = orig
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Maintain = %v, want check failure", err)
	}
	if pageStats(t, p).PageSize != 4096 || fingerprint(t, p) != before {
		t.Error("failed maintenance changed the archive")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), "maint") {
			t.Errorf("leftover %s", e.Name())
		}
	}
	// The archive is writable again (the lock was released).
	s2, err := Open(p, WithLockDir(filepath.Join(dir, "locks")))
	if err != nil {
		t.Fatal(err)
	}
	_ = s2.Close()

	// A backup that cannot be written aborts before anything else.
	o := maintainOpts(dir)
	o.BackupDir = p // a file, not a directory
	if _, err := Maintain(ctx, p, o); err == nil {
		t.Error("Maintain with an unusable backup dir succeeded")
	}
	if fingerprint(t, p) != before {
		t.Error("failed backup changed the archive")
	}
}

func TestMaintainWaitsForWriters(t *testing.T) {
	s, p := openTestStore(t)
	fillStore(t, s, "a", 20)
	_ = s.Close()
	dir := filepath.Dir(p)
	lockDir := filepath.Join(dir, "locks")

	w, err := Open(p, WithLockDir(lockDir))
	if err != nil {
		t.Fatal(err)
	}
	o := maintainOpts(dir)
	o.Wait = 0
	if _, err := Maintain(ctx, p, o); !errors.Is(err, ErrWritersActive) {
		t.Errorf("Maintain with an open writer = %v, want ErrWritersActive", err)
	}
	// With a wait, maintenance proceeds once the writer closes.
	o.Wait = 5 * time.Second
	go func() {
		time.Sleep(200 * time.Millisecond)
		fillStore(t, w, "late", 3)
		_ = w.Close()
	}()
	res, err := Maintain(ctx, p, o)
	if err != nil {
		t.Fatalf("Maintain after writer closed: %v", err)
	}
	if res.After.Events != 23 {
		t.Errorf("events after = %d, want 23 (writes before the swap must be kept)", res.After.Events)
	}
}

func TestOpenRefusedDuringMaintenance(t *testing.T) {
	_, p := openTestStore(t)
	lockDir := filepath.Join(filepath.Dir(p), "locks")
	release, err := lockMaintenance(lockDir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := Open(p, WithLockDir(lockDir)); !errors.Is(err, ErrMaintenance) {
		t.Errorf("Open during maintenance = %v, want ErrMaintenance", err)
	}
	// Readers are not affected, and a writer without a lock dir is not either
	// (tools that manage their own archives).
	r, err := OpenReadOnly(p)
	if err != nil {
		t.Errorf("OpenReadOnly during maintenance: %v", err)
	} else {
		_ = r.Close()
	}
	w, err := Open(p)
	if err != nil {
		t.Errorf("Open without lock dir during maintenance: %v", err)
	} else {
		_ = w.Close()
	}
	// Two writers share the lock.
	release()
	a, err := Open(p, WithLockDir(lockDir))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(p, WithLockDir(lockDir))
	if err != nil {
		t.Fatalf("second writer: %v", err)
	}
	_ = a.Close()
	_ = b.Close()
}

// TestMaintainPendingStopsNewWriters: once maintenance waits for an open
// writer, a new writer is refused, so maintenance cannot starve under
// continuous writing. The sequence is driven by observation, not timing.
func TestMaintainPendingStopsNewWriters(t *testing.T) {
	s, p := openTestStore(t)
	fillStore(t, s, "a", 20)
	_ = s.Close()
	dir := filepath.Dir(p)
	lockDir := filepath.Join(dir, "locks")

	w, err := Open(p, WithLockDir(lockDir))
	if err != nil {
		t.Fatal(err)
	}
	o := maintainOpts(dir)
	o.Wait = 30 * time.Second
	done := make(chan error, 1)
	go func() {
		_, err := Maintain(ctx, p, o)
		done <- err
	}()
	// Wait until maintenance has announced itself: a new writer is refused.
	deadline := time.Now().Add(10 * time.Second)
	for {
		n, err := Open(p, WithLockDir(lockDir))
		if errors.Is(err, ErrMaintenance) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		_ = n.Close()
		if time.Now().After(deadline) {
			t.Fatal("new writers are still admitted while maintenance waits")
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("maintenance finished while a writer was open: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	// A second maintenance run is refused as well.
	o2 := o
	o2.Wait = 0
	if _, err := Maintain(ctx, p, o2); !errors.Is(err, ErrMaintenance) {
		t.Errorf("second maintenance = %v, want ErrMaintenance", err)
	}
	fillStore(t, w, "late", 2)
	_ = w.Close()
	if err := <-done; err != nil {
		t.Fatalf("Maintain: %v", err)
	}
	if ps := pageStats(t, p); ps.PageSize != 16384 || ps.Events != 22 {
		t.Errorf("after = %+v", ps)
	}
	// Both locks are released: a writer opens again.
	n, err := Open(p, WithLockDir(lockDir))
	if err != nil {
		t.Fatal(err)
	}
	_ = n.Close()
}

// TestMaintainLosesNoWrites runs a writer that keeps appending through two
// maintenance runs, reopening at once after every write. Every write that
// was acknowledged must be in the final archive; a writer that meets the
// maintenance lock retries.
func TestMaintainLosesNoWrites(t *testing.T) {
	s, p := openTestStore(t)
	fillStore(t, s, "a", 50)
	_ = s.Close()
	dir := filepath.Dir(p)
	lockDir := filepath.Join(dir, "locks")

	var mu sync.Mutex
	var written []string
	refused := 0
	stop := make(chan struct{})
	done := make(chan error, 1)
	// wrote signals every acknowledged write, so the test waits for the
	// writer's progress instead of for the clock.
	wrote := make(chan struct{}, 1<<16)
	awaitWrites := func(n int) {
		t.Helper()
		for ; n > 0; n-- {
			select {
			case <-wrote:
			case err := <-done:
				t.Fatalf("writer stopped early: %v", err)
			case <-time.After(2 * time.Minute):
				t.Fatal("writer made no progress")
			}
		}
	}
	go func() {
		i := 0
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			w, err := Open(p, WithLockDir(lockDir))
			if errors.Is(err, ErrMaintenance) {
				mu.Lock()
				refused++
				mu.Unlock()
				time.Sleep(time.Millisecond)
				continue
			}
			if err != nil {
				done <- err
				return
			}
			id := fmt.Sprintf("claude:w-%d", i)
			e := ev(id, "claude:w", "2026-11-01T10:00:00Z", model.ActorAgent, model.KindMessage, "assistant", strings.Repeat("x", 2000))
			err = w.WriteBatch(ctx, SourceState{Path: "/src/w.jsonl", Harness: "claude", Offset: int64(i)}, nil, []model.Event{e})
			_ = w.Close()
			if err != nil {
				done <- err
				return
			}
			mu.Lock()
			written = append(written, id)
			mu.Unlock()
			select {
			case wrote <- struct{}{}:
			default:
			}
			i++
		}
	}()
	for round := 0; round < 2; round++ {
		awaitWrites(3) // the writer is running before maintenance starts
		o := maintainOpts(dir)
		o.Wait = 30 * time.Second
		if round == 1 {
			o.PageSize = 8192
		}
		if _, err := Maintain(ctx, p, o); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}
	awaitWrites(3) // and still writes once maintenance is over
	close(stop)
	if err := <-done; err != nil {
		t.Fatalf("writer: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(written) == 0 {
		t.Fatal("writer wrote nothing")
	}
	r, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if ps, _ := r.PageStats(ctx); ps.PageSize != 8192 {
		t.Errorf("page size = %d", ps.PageSize)
	}
	for _, id := range written {
		var n int
		if err := r.db().QueryRowContext(ctx, "SELECT count(*) FROM events WHERE id = ?", id).Scan(&n); err != nil || n != 1 {
			t.Errorf("acknowledged write %s missing after maintenance (%v)", id, err)
		}
	}
	t.Logf("%d writes, %d refused during maintenance", len(written), refused)
}

func TestMaintainDiskSpace(t *testing.T) {
	s, p := openTestStore(t)
	fillStore(t, s, "a", 20)
	_ = s.Close()
	dir := filepath.Dir(p)
	before := fingerprint(t, p)
	orig := freeSpace
	defer func() { freeSpace = orig }()
	freeSpace = func(string) (int64, error) { return 1024, nil }
	for _, dry := range []bool{false, true} {
		o := maintainOpts(dir)
		o.DryRun = dry
		_, err := Maintain(ctx, p, o)
		if !errors.Is(err, ErrDiskSpace) || !strings.Contains(err.Error(), "MiB") {
			t.Errorf("dry=%v: err = %v, want ErrDiskSpace with sizes", dry, err)
		}
	}
	if fingerprint(t, p) != before {
		t.Error("archive changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); err == nil {
		if ents, _ := os.ReadDir(filepath.Join(dir, "backups")); len(ents) > 0 {
			t.Error("backup written despite missing disk space")
		}
	}
	// The backup dir is checked too.
	freeSpace = func(d string) (int64, error) {
		if d == dir {
			return 1 << 40, nil
		}
		return 0, nil
	}
	if _, err := Maintain(ctx, p, maintainOpts(dir)); !errors.Is(err, ErrDiskSpace) || !strings.Contains(err.Error(), "backup dir") {
		t.Errorf("full backup dir: err = %v", err)
	}
}

func TestMaintainRemovesLeftoversAndCleansUpOnCancel(t *testing.T) {
	s, p := openTestStore(t)
	fillStore(t, s, "a", 50)
	_ = s.Close()
	dir := filepath.Dir(p)
	stale := p + ".maint-stale"
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	leftovers := func() []string {
		m, _ := filepath.Glob(p + ".maint-*")
		return m
	}

	// Cancelled while the copy exists (an interrupt during the check): the
	// copy is removed, the archive untouched, also in a dry run.
	orig := checkCopy
	defer func() { checkCopy = orig }()
	for _, dry := range []bool{true, false} {
		cctx, cancel := context.WithCancel(ctx)
		var seen string
		checkCopy = func(ctx context.Context, path string, want PageStats) (PageStats, error) {
			seen = path
			if _, err := os.Stat(path); err != nil {
				t.Errorf("copy %s missing during check: %v", path, err)
			}
			cancel()
			return PageStats{}, ctx.Err()
		}
		o := maintainOpts(dir)
		o.DryRun = dry
		_, err := Maintain(cctx, p, o)
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Errorf("dry=%v: err = %v, want context.Canceled", dry, err)
		}
		if seen == "" {
			t.Fatalf("dry=%v: check not called", dry)
		}
		if l := leftovers(); len(l) != 0 {
			t.Errorf("dry=%v: leftovers %v", dry, l)
		}
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Error("stale copy of an earlier run not removed")
	}
	if pageStats(t, p).PageSize != 4096 {
		t.Error("archive changed")
	}
	// A cancelled context stops the rebuild itself before any copy is made.
	checkCopy = orig
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Maintain(cctx, p, maintainOpts(dir)); err == nil {
		t.Error("Maintain with a cancelled context succeeded")
	}
	if l := leftovers(); len(l) != 0 {
		t.Errorf("leftovers %v", l)
	}
}
