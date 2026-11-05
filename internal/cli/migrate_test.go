package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/store"
	tf "github.com/seletz/total-recall/internal/testfixture"
)

func itoa(n int) string { return strconv.Itoa(n) }

// outdatedDB replaces the test env's archive with an empty one at schema v1,
// as a user who upgraded the binary has.
func (e *testEnv) outdatedDB(t *testing.T) {
	t.Helper()
	_ = os.Remove(e.db)
	if err := store.CreateAtVersion(e.db, 1); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) schemaVersion(t *testing.T) int {
	t.Helper()
	return e.count(t, "SELECT max(version) FROM schema_version")
}

// TestNoAutoMigration: no write command migrates an older archive; each
// fails with the advice and changes nothing. backup works, as advised.
func TestNoAutoMigration(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"ingest --all", []string{"ingest", "--all"}},
		{"ingest path", nil}, // filled in below
		{"reindex", []string{"reindex", "--actors"}},
		{"maintenance", []string{"maintenance"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			e.outdatedDB(t)
			args := tt.args
			if args == nil {
				args = []string{"ingest", filepath.Join(e.projects, "-w-beta", tf.SmallSession2+".jsonl")}
			}
			r := e.run(t, "", args...)
			if r.code == 0 {
				t.Fatalf("%v on an outdated archive succeeded:\n%s", args, r.stdout)
			}
			for _, want := range []string{"older than this binary", "total-recall backup", "total-recall migrate"} {
				if !strings.Contains(r.stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, r.stderr)
				}
			}
			if v := e.schemaVersion(t); v != 1 {
				t.Errorf("schema version = %d, the command migrated", v)
			}
			if n := e.count(t, "SELECT count(*) FROM sources"); n != 0 {
				t.Errorf("sources = %d, the command wrote", n)
			}
			if m, _ := filepath.Glob(e.db + ".pre-v*.bak"); len(m) != 0 {
				t.Errorf("pre-migration copy written: %v", m)
			}
		})
	}
	t.Run("backup works", func(t *testing.T) {
		e := newTestEnv(t)
		e.outdatedDB(t)
		out := e.mustRun(t, "backup")
		if !strings.Contains(out, "backup written: "+filepath.Join(e.backups, "archive-2026-10-20.db.gz")) {
			t.Errorf("backup output: %s", out)
		}
		if v := e.schemaVersion(t); v != 1 {
			t.Errorf("backup migrated to v%d", v)
		}
	})
	t.Run("json error kind db", func(t *testing.T) {
		e := newTestEnv(t)
		e.outdatedDB(t)
		r := e.run(t, "", "maintenance", "--json")
		if r.code == 0 || !strings.Contains(r.stdout, `"kind":"db"`) || !strings.Contains(r.stdout, "total-recall migrate") {
			t.Errorf("maintenance --json = %+v", r)
		}
	})
}

// TestHookOutdatedArchive: a hook on an older archive writes nothing, logs
// one WARN line per day, exits 0, and the first hook after the migration
// reads everything that was skipped.
func TestHookOutdatedArchive(t *testing.T) {
	e := newTestEnv(t)
	e.outdatedDB(t)
	for i := 0; i < 3; i++ {
		r := e.run(t, e.hookPayload(t, "Stop"), "ingest", "--hook")
		if r.code != 0 || r.stdout != "" {
			t.Fatalf("hook run %d = %+v", i, r)
		}
	}
	log := e.logText(t)
	if strings.Contains(log, " ERROR ") {
		t.Errorf("hook logged an error:\n%s", log)
	}
	if n := strings.Count(log, " WARN "); n != 1 || !strings.Contains(log, "needs a migration") || !strings.Contains(log, "total-recall migrate") {
		t.Errorf("want exactly one WARN line with the advice, got %d:\n%s", n, log)
	}
	// The next day warns again.
	e.now = e.now.Add(24 * time.Hour)
	e.run(t, e.hookPayload(t, "Stop"), "ingest", "--hook")
	if n := strings.Count(e.logText(t), " WARN "); n != 2 {
		t.Errorf("WARN lines after a day = %d, want 2", n)
	}
	if e.count(t, "SELECT count(*) FROM sources") != 0 || e.count(t, "SELECT count(*) FROM events") != 0 || e.schemaVersion(t) != 1 {
		t.Fatal("the hook wrote or migrated")
	}

	e.mustRun(t, "backup")
	e.mustRun(t, "migrate", "--yes")
	if r := e.run(t, e.hookPayload(t, "SessionStart"), "ingest", "--hook"); r.code != 0 {
		t.Fatal(r)
	}
	if n := e.count(t, "SELECT count(*) FROM events"); n != 9 {
		t.Errorf("events after migrate and SessionStart = %d, want all 9", n)
	}
	if strings.Contains(e.logText(t), " ERROR ") {
		t.Errorf("errors logged:\n%s", e.logText(t))
	}
}

func TestMigrateCommand(t *testing.T) {
	latest := store.LatestSchemaVersion()
	t.Run("no archive", func(t *testing.T) {
		e := newTestEnv(t)
		if r := e.run(t, "", "migrate"); r.code == 0 || !strings.Contains(r.stderr, "no archive") {
			t.Errorf("migrate without DB = %+v", r)
		}
	})
	t.Run("current archive", func(t *testing.T) {
		e := newTestEnv(t)
		e.mustRun(t, "ingest", "--all")
		out := e.mustRun(t, "migrate")
		if !strings.Contains(out, "is current, nothing to migrate") || strings.Contains(out, "Migrate now?") {
			t.Errorf("migrate on a current DB:\n%s", out)
		}
		m := decode[MigrateJSON](t, e.mustRun(t, "migrate", "--json"))
		if m.Pending || m.Migrated || m.From != latest || m.To != latest || m.Backup != "" || len(m.Steps) != 0 || m.DB != e.db {
			t.Errorf("json = %+v", m)
		}
		if m, _ := filepath.Glob(e.db + ".pre-v*.bak"); len(m) != 0 {
			t.Errorf("copy written on a current DB: %v", m)
		}
	})
	t.Run("dry run changes nothing", func(t *testing.T) {
		e := newTestEnv(t)
		e.outdatedDB(t)
		out := e.mustRun(t, "migrate", "--dry-run")
		for _, want := range []string{"v1 -> v" + itoa(latest), "pending migration", "  v2   ", "backup    " + e.db + ".pre-v1.bak", "free needed", "dry run: nothing changed"} {
			if !strings.Contains(out, want) {
				t.Errorf("dry run lacks %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "Migrate now?") {
			t.Error("dry run asked for confirmation")
		}
		m := decode[MigrateJSON](t, e.mustRun(t, "migrate", "--dry-run", "--json"))
		if !m.DryRun || !m.Pending || m.Migrated || m.From != 1 || m.To != latest || len(m.Steps) != latest-1 || m.Steps[0].Version != 2 ||
			m.Steps[0].Summary == "" || string(m.Backup) != e.db+".pre-v1.bak" || m.NeedBytes <= m.SizeBytes {
			t.Errorf("json = %+v", m)
		}
		if e.schemaVersion(t) != 1 {
			t.Error("dry run migrated")
		}
		if _, err := os.Stat(e.db + ".pre-v1.bak"); err == nil {
			t.Error("dry run wrote the copy")
		}
	})
	t.Run("declined changes nothing", func(t *testing.T) {
		for _, answer := range []string{"", "n\n", "no\n", "nein\n"} {
			e := newTestEnv(t)
			e.outdatedDB(t)
			r := e.run(t, answer, "migrate")
			if r.code != 1 || !strings.Contains(r.stdout, "Migrate now?") || !strings.Contains(r.stderr, "cancelled, the archive is unchanged") {
				t.Errorf("answer %q: %+v", answer, r)
			}
			if e.schemaVersion(t) != 1 {
				t.Errorf("answer %q migrated", answer)
			}
			if _, err := os.Stat(e.db + ".pre-v1.bak"); err == nil {
				t.Errorf("answer %q wrote the copy", answer)
			}
		}
	})
	t.Run("confirmed migrates", func(t *testing.T) {
		e := newTestEnv(t)
		e.outdatedDB(t)
		r := e.run(t, "Y\n", "migrate")
		if r.code != 0 {
			t.Fatalf("%+v", r)
		}
		for _, want := range []string{"Migrate now?", "backup written to " + e.db + ".pre-v1.bak", "migration v2 applied", "migration v" + itoa(latest) + " applied", "migrated v1 -> v" + itoa(latest)} {
			if !strings.Contains(r.stdout, want) {
				t.Errorf("output lacks %q:\n%s", want, r.stdout)
			}
		}
		if v := e.schemaVersion(t); v != latest {
			t.Errorf("version after migrate = %d", v)
		}
		if _, err := os.Stat(e.db + ".pre-v1.bak"); err != nil {
			t.Error(err)
		}
		// Idempotent, and ingest works again.
		if out := e.mustRun(t, "migrate", "--yes"); !strings.Contains(out, "is current") {
			t.Errorf("second migrate:\n%s", out)
		}
		e.mustRun(t, "ingest", "--all")
	})
	t.Run("--yes --json", func(t *testing.T) {
		e := newTestEnv(t)
		e.outdatedDB(t)
		m := decode[MigrateJSON](t, e.mustRun(t, "migrate", "--yes", "--json"))
		if !m.Migrated || m.DryRun || m.From != 1 || m.To != latest || string(m.Backup) != e.db+".pre-v1.bak" {
			t.Errorf("json = %+v", m)
		}
		if e.schemaVersion(t) != latest {
			t.Error("not migrated")
		}
	})
	t.Run("writer holds the archive", func(t *testing.T) {
		e := newTestEnv(t)
		e.outdatedDB(t)
		w, err := store.Open(e.db, store.AllowOutdated(), store.WithLockDir(filepath.Join(filepath.Dir(e.db), "locks")))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = w.Close() }()
		r := e.run(t, "", "migrate", "--yes", "--wait", "0", "--json")
		if r.code == 0 || !strings.Contains(r.stdout, `"kind":"db"`) || !strings.Contains(r.stderr, "writers active") {
			t.Errorf("migrate with a writer = %+v", r)
		}
		if e.schemaVersion(t) != 1 {
			t.Error("migrated despite the writer")
		}
	})
}

// TestOutdatedReadCommands: reading works as far as the schema allows, with a
// warning; status and doctor say loudly that a migration is pending.
func TestOutdatedReadCommands(t *testing.T) {
	e := newTestEnv(t)
	e.outdatedDB(t)
	latest := store.LatestSchemaVersion()

	r := e.run(t, "", "status")
	if r.code != 0 {
		t.Fatalf("status = %+v", r)
	}
	if !strings.Contains(r.stdout, "v1 (binary supports v"+itoa(latest)+")  MIGRATION PENDING: run `total-recall backup`, then `total-recall migrate`") {
		t.Errorf("status lacks the pending hint:\n%s", r.stdout)
	}
	if !strings.Contains(r.stderr, "warning: database schema v1 is older") {
		t.Errorf("status stderr: %s", r.stderr)
	}
	st := decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
	if !st.MigrationPending || st.SchemaVersion != 1 || st.BinarySchemaVersion != latest {
		t.Errorf("status json = %+v", st)
	}
	st = decode[StatusJSON](t, func() string {
		e2 := newTestEnv(t)
		e2.mustRun(t, "ingest", "--all")
		return e2.mustRun(t, "status", "--json")
	}())
	if st.MigrationPending {
		t.Error("migration_pending on a current DB")
	}

	// sessions needs columns of later migrations: the error names the way out.
	r = e.run(t, "", "sessions", "--json")
	if r.code == 0 || !strings.Contains(r.stderr, "no such column") || !strings.Contains(r.stderr, "run `total-recall backup`, then `total-recall migrate`") {
		t.Errorf("sessions on v1 = %+v", r)
	}
	// sql with v1 columns works, with the warning.
	r = e.run(t, "", "sql", "SELECT count(*) FROM events")
	if r.code != 0 || !strings.Contains(r.stderr, "warning:") {
		t.Errorf("sql on v1 = %+v", r)
	}

	r = e.run(t, "", "doctor")
	if r.code != 2 || !strings.Contains(r.stdout, "MIGRATION PENDING: the archive has schema v1, this binary v"+itoa(latest)) {
		t.Errorf("doctor on v1 = %+v", r)
	}
	r = e.run(t, "", "doctor", "--json")
	if r.code != 2 || !strings.Contains(r.stderr, "MIGRATION PENDING") || strings.Contains(r.stdout, "MIGRATION") {
		t.Errorf("doctor --json on v1 = %+v", r)
	}
}
