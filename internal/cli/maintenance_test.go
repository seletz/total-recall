package cli

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/seletz/total-recall/internal/store"
)

func TestMaintenance(t *testing.T) {
	e := newTestEnv(t)
	if r := e.run(t, "", "maintenance"); r.code == 0 || !strings.Contains(r.stderr, "no archive") {
		t.Errorf("maintenance without DB = %+v", r)
	}
	e.mustRun(t, "ingest", "--all")
	events := e.count(t, "SELECT count(*) FROM events")

	// status shows the page size and recommends the rebuild.
	st := decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
	if st.PageSize != 4096 {
		t.Errorf("status page_size = %d", st.PageSize)
	}
	if out := e.mustRun(t, "status"); !strings.Contains(out, "page size             4096  (total-recall maintenance") {
		t.Errorf("status lacks the page size hint:\n%s", out)
	}

	// Dry run: measured, nothing changed, no backup.
	m := decode[MaintenanceJSON](t, e.mustRun(t, "maintenance", "--dry-run", "--json"))
	if !m.DryRun || m.Swapped || m.Backup != "" || m.PageSize != 16384 || m.Before.PageSize != 4096 || m.After.PageSize != 16384 || m.After.Events != events || m.DB != e.db {
		t.Errorf("dry run = %+v", m)
	}
	if ents, _ := os.ReadDir(e.backups); len(ents) != 0 {
		t.Error("dry run wrote a backup")
	}
	if e.count(t, "PRAGMA page_size") != 4096 {
		t.Error("dry run changed the archive")
	}
	if out := e.mustRun(t, "maintenance", "--dry-run"); !strings.Contains(out, "dry run: the archive was not changed") || !strings.Contains(out, "BEFORE") {
		t.Errorf("dry run text:\n%s", out)
	}

	// The real thing: backup, progress lines, before/after table.
	out := e.mustRun(t, "maintenance")
	for _, want := range []string{"backup written: " + filepath.Join(e.backups, "archive-2026-10-20.db.gz"), "rebuilding with page size 16384", "archive replaced: " + e.db, "page size", "4096", "16384", "events"} {
		if !strings.Contains(out, want) {
			t.Errorf("maintenance output lacks %q:\n%s", want, out)
		}
	}
	if e.count(t, "PRAGMA page_size") != 16384 || e.count(t, "SELECT count(*) FROM events") != events {
		t.Error("archive not rebuilt or data lost")
	}
	if _, err := os.Stat(filepath.Join(e.backups, "archive-2026-10-20.db.gz")); err != nil {
		t.Error(err)
	}
	st = decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
	if st.PageSize != 16384 {
		t.Errorf("status page_size after = %d", st.PageSize)
	}
	if out := e.mustRun(t, "status"); !strings.Contains(out, "page size             16384\n") {
		t.Errorf("status after:\n%s", out)
	}
	// Ingest still works on the rebuilt archive.
	e.mustRun(t, "ingest", "--all")

	// A second run with --json and another size.
	m = decode[MaintenanceJSON](t, e.mustRun(t, "maintenance", "--json", "--page-size", "8192"))
	if !m.Swapped || m.DryRun || m.Backup == "" || m.After.PageSize != 8192 || m.Before.PageSize != 16384 || m.After.Events != events {
		t.Errorf("second run = %+v", m)
	}

	// Bad page sizes are usage errors, also as JSON.
	r := e.run(t, "", "maintenance", "--page-size", "1000", "--json")
	if r.code == 0 || !strings.Contains(r.stdout, `"usage"`) || !strings.Contains(r.stderr, "power of two") {
		t.Errorf("bad page size = %+v", r)
	}
}

func TestMaintenanceLockSkipsHooks(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	// Hold the maintenance lock as a running maintenance would.
	lock := filepath.Join(filepath.Dir(e.db), "locks", store.MaintenanceLockName)
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	r := e.run(t, e.hookPayload(t, "Stop"), "ingest", "--hook")
	if r.code != 0 || r.stdout != "" {
		t.Errorf("hook during maintenance = %+v", r)
	}
	if log := e.logText(t); !strings.Contains(log, "INFO") || !strings.Contains(log, "maintenance in progress, skipped") || strings.Contains(log, "ERROR") {
		t.Errorf("log:\n%s", log)
	}
	st := decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
	if st.RecentErrorsTotal != 0 {
		t.Errorf("skipped hook counted as error: %+v", st.RecentErrors)
	}
	// Explicit write commands fail with a clear message; reads work.
	for _, args := range [][]string{{"ingest", "--all"}, {"backup"}, {"reindex", "--actors"}} {
		if r := e.run(t, "", args...); r.code == 0 || !strings.Contains(r.stderr, "maintenance in progress") {
			t.Errorf("%v during maintenance = %+v", args, r)
		}
	}
	e.mustRun(t, "sessions")
	// Maintenance itself does not wait for a second maintenance run.
	if r := e.run(t, "", "maintenance", "--wait", "0"); r.code == 0 || !strings.Contains(r.stderr, "writers active") {
		t.Errorf("maintenance while locked = %+v", r)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	e.mustRun(t, "ingest", "--all")
}
