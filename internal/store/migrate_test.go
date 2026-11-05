package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openAtVersion creates a database at path with only migrations 1..k applied.
func openAtVersion(t *testing.T, path string, k int) {
	t.Helper()
	if err := CreateAtVersion(path, k); err != nil {
		t.Fatalf("create at version %d: %v", k, err)
	}
}

// sampleRows inserts rows using only columns that exist since version 1, so
// they are valid at every historical version.
var sampleRows = []string{
	`INSERT INTO sources (path, harness, size, mtime, "offset", ingested_at)
		VALUES ('/p/a.jsonl', 'claude', 100, '2026-10-01T10:00:00.000Z', 90, '2026-10-01T10:00:01.000Z')`,
	`INSERT INTO sessions (id, harness, native_id, name, agent_name, cwd, git_remote_url, started_at, ended_at)
		VALUES ('claude:s1', 'claude', 's1', 'Title', 'agent', '/w', 'https://github.com/o/r',
			'2026-10-01T10:00:00.000Z', '2026-10-01T11:00:00.000Z')`,
	// s0 is the session s1 continues (migration 4 adds the link columns;
	// the test sets them after migrating).
	`INSERT INTO sessions (id, harness, native_id, name, cwd, started_at, ended_at)
		VALUES ('claude:s0', 'claude', 's0', 'Earlier', '/w', '2026-09-30T10:00:00.000Z', '2026-09-30T11:00:00.000Z')`,
	`INSERT INTO events (id, session_id, ts, kind, role, actor, text, source_path, source_offset, raw)
		VALUES ('claude:e1', 'claude:s1', '2026-10-01T10:00:00.000Z', 'message', 'user', 'human',
			'hello migration world', '/p/a.jsonl', 0, '{}')`,
	`INSERT INTO dirs (cwd, git_root, git_remote_url, resolved_at)
		VALUES ('/w', '/w', 'https://github.com/o/r', '2026-10-01T10:00:00.000Z')`,
}

// TestMigrateFromEveryVersion builds a database at every historical schema
// version, fills it, migrates to the latest version and checks that the data
// survived and a pre-migration backup was written. New migrations are
// covered automatically.
func TestMigrateFromEveryVersion(t *testing.T) {
	for k := 1; k <= LatestSchemaVersion(); k++ {
		t.Run(fmt.Sprintf("from v%d", k), func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "archive.db")
			openAtVersion(t, p, k)
			db, err := sql.Open("sqlite", dsn(p))
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range sampleRows {
				if _, err := db.ExecContext(ctx, q); err != nil {
					t.Fatalf("v%d: %s: %v", k, q, err)
				}
			}
			_ = db.Close()

			// Open never migrates an existing database: an older one is
			// refused and left as it is.
			if k < LatestSchemaVersion() {
				if _, err := Open(p); !errors.Is(err, ErrSchemaOutdated) {
					t.Fatalf("Open on v%d = %v, want ErrSchemaOutdated", k, err)
				}
				plan, err := PlanMigration(ctx, p)
				if err != nil || plan.From != k || plan.To != LatestSchemaVersion() || len(plan.Steps) != LatestSchemaVersion()-k ||
					plan.Steps[0].Version != k+1 || plan.Backup != fmt.Sprintf("%s.pre-v%d.bak", p, k) || plan.NeedBytes <= plan.SizeBytes || plan.SizeBytes == 0 {
					t.Errorf("PlanMigration = %+v (%v)", plan, err)
				}
			}
			var logged []string
			logf := func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
			res, err := Migrate(ctx, p, MigrateOptions{LockDir: filepath.Join(filepath.Dir(p), "locks"), Logf: logf})
			if err != nil {
				t.Fatalf("Migrate: %v", err)
			}
			if res.Plan.From != k || res.Plan.To != LatestSchemaVersion() {
				t.Errorf("result plan = %+v", res.Plan)
			}
			s, err := Open(p)
			if err != nil {
				t.Fatalf("Open after Migrate: %v", err)
			}
			defer func() { _ = s.Close() }()
			if v, _ := s.SchemaVersion(ctx); v != LatestSchemaVersion() {
				t.Errorf("version = %d, want %d", v, LatestSchemaVersion())
			}
			if s.MigrationPending() {
				t.Error("MigrationPending after Migrate")
			}
			checks := map[string]int{
				"SELECT count(*) FROM sources WHERE \"offset\" = 90":                        1,
				"SELECT count(*) FROM sessions WHERE name = 'Title' AND cwd = '/w'":         1,
				"SELECT count(*) FROM events WHERE text = 'hello migration world'":          1,
				"SELECT count(*) FROM events_fts WHERE events_fts MATCH 'migration'":        1,
				"SELECT count(*) FROM dirs WHERE git_root = '/w'":                           1,
				"SELECT count(*) FROM events WHERE raw_truncated = 0 AND raw = '{}'":        1,
				"SELECT count(*) FROM exchanges WHERE request = 'hello migration world'":    1,
				"SELECT count(*) FROM daily_activity WHERE repo = 'https://github.com/o/r'": 1,
			}
			for q, want := range checks {
				if got := countRows(t, s, q); got != want {
					t.Errorf("%s = %d, want %d", q, got, want)
				}
			}
			if _, err := s.Session(ctx, "claude:s1"); err != nil {
				t.Errorf("Session after migration: %v", err)
			}
			// Migration 4: the link and account columns are usable and indexed,
			// and the reverse link is derived from them.
			if _, err := s.db().ExecContext(ctx, `UPDATE sessions SET continued_in = 'claude:s1', account_id = 'acc-1',
				organization_id = 'org-1' WHERE id = 'claude:s0'`); err != nil {
				t.Fatalf("set v4 columns: %v", err)
			}
			v4 := map[string]int{
				"SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name IN ('sessions_continued_in', 'sessions_account_id')": 2,
				"SELECT count(*) FROM sessions WHERE continued_in IS NULL AND account_id IS NULL AND organization_id IS NULL":          1,
			}
			for q, want := range v4 {
				if got := countRows(t, s, q); got != want {
					t.Errorf("%s = %d, want %d", q, got, want)
				}
			}
			if si, err := s.Session(ctx, "claude:s1"); err != nil || si.ContinuedFrom != "claude:s0" {
				t.Errorf("s1 after migration: continued_from %q (%v), want claude:s0", si.ContinuedFrom, err)
			}
			if si, err := s.Session(ctx, "claude:s0"); err != nil || si.ContinuedIn != "claude:s1" || si.AccountID != "acc-1" || si.OrganizationID != "org-1" {
				t.Errorf("s0 after migration: %+v (%v)", si.Session, err)
			}
			if list, err := s.Sessions(ctx, SessionFilter{Account: "acc"}); err != nil || ids(list) != "claude:s0" {
				t.Errorf("Sessions by account after migration = %q (%v)", ids(list), err)
			}

			bak := PreMigrationBackup(p, k)
			_, statErr := os.Stat(bak)
			if k < LatestSchemaVersion() {
				if statErr != nil {
					t.Errorf("no pre-migration backup %s: %v", bak, statErr)
				}
				if len(logged) == 0 || !strings.Contains(strings.Join(logged, "\n"), bak) {
					t.Errorf("backup not logged: %q", logged)
				}
				if !strings.Contains(strings.Join(logged, "\n"), fmt.Sprintf("migration v%d applied", LatestSchemaVersion())) {
					t.Errorf("migrations not logged: %q", logged)
				}
			} else {
				if statErr == nil {
					t.Errorf("unexpected backup %s for up-to-date DB", bak)
				}
				if len(logged) != 0 || res.Duration != 0 {
					t.Errorf("up-to-date DB: logged %q, duration %s", logged, res.Duration)
				}
			}
		})
	}
}

func TestNoBackupForFreshDB(t *testing.T) {
	_, p := openTestStore(t)
	matches, _ := filepath.Glob(p + ".pre-v*.bak")
	if len(matches) != 0 {
		t.Errorf("fresh DB got backups: %v", matches)
	}
}

func TestOpenReadOnlySchemaVersion(t *testing.T) {
	t.Run("newer is an error", func(t *testing.T) {
		s, p := openTestStore(t)
		if _, err := s.db().ExecContext(ctx, "UPDATE schema_version SET version = 999"); err != nil {
			t.Fatal(err)
		}
		_ = s.Close()
		_, err := OpenReadOnly(p)
		if !errors.Is(err, ErrSchemaTooNew) {
			t.Fatalf("OpenReadOnly = %v, want ErrSchemaTooNew", err)
		}
		if _, err := Open(p); !errors.Is(err, ErrSchemaTooNew) {
			t.Fatalf("Open = %v, want ErrSchemaTooNew", err)
		}
	})
	t.Run("older works and says how to migrate", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "archive.db")
		openAtVersion(t, p, 1)
		s, err := OpenReadOnly(p)
		if err != nil {
			t.Fatalf("OpenReadOnly: %v", err)
		}
		defer func() { _ = s.Close() }()
		if LatestSchemaVersion() > 1 {
			w := s.SchemaWarning()
			if !strings.Contains(w, "older") || !strings.Contains(w, "total-recall migrate") {
				t.Errorf("SchemaWarning = %q", w)
			}
		}
		if _, _, err := s.Query(ctx, "SELECT count(*) FROM sessions"); err != nil {
			t.Errorf("query on older DB: %v", err)
		}
		if v, _ := s.SchemaVersion(ctx); v != 1 {
			t.Errorf("read-only open migrated the DB: version %d", v)
		}
	})
	t.Run("current has no warning", func(t *testing.T) {
		_, p := openTestStore(t)
		s, err := OpenReadOnly(p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()
		if w := s.SchemaWarning(); w != "" {
			t.Errorf("SchemaWarning = %q", w)
		}
	})
}

// TestMigrationNotes checks that every migration has a description and that
// Go steps only exist for versions that exist.
func TestMigrationNotes(t *testing.T) {
	if len(migrationNotes) != len(migrations) {
		t.Fatalf("%d migration notes for %d migrations: describe every migration", len(migrationNotes), len(migrations))
	}
	for i, n := range migrationNotes {
		if strings.TrimSpace(n.Summary) == "" || n.Space < 0 {
			t.Errorf("note of migration %d: %+v", i+1, n)
		}
	}
	for v := range migrationSteps {
		if v < 1 || v > len(migrations) {
			t.Errorf("Go step for migration %d, which does not exist", v)
		}
	}
	all := PendingMigrations(0)
	if len(all) != len(migrations) || all[0].Version != 1 || all[len(all)-1].Version != len(migrations) {
		t.Errorf("PendingMigrations(0) = %+v", all)
	}
	if got := PendingMigrations(len(migrations)); len(got) != 0 || got == nil {
		t.Errorf("PendingMigrations(latest) = %#v, want an empty, non-nil list", got)
	}
}

// withTestMigration appends an artificial migration with a Go step for the
// duration of the test: it adds a column and rewrites every event through
// the step, reporting progress. It stands in for migrations like v5 that
// rewrite data.
func withTestMigration(t *testing.T, space float64, step migrationStep) int {
	t.Helper()
	savedM, savedN := migrations, migrationNotes
	migrations = append(migrations[:len(migrations):len(migrations)], "ALTER TABLE events ADD COLUMN test_col TEXT;")
	migrationNotes = append(migrationNotes[:len(migrationNotes):len(migrationNotes)], migrationNote{Summary: "events: test_col, rewrites every event; takes a while", Space: space})
	v := len(migrations)
	if step != nil {
		migrationSteps[v] = step
	}
	t.Cleanup(func() {
		migrations, migrationNotes = savedM, savedN
		delete(migrationSteps, v)
	})
	return v
}

// rewriteEvents is a Go step that touches every event and reports progress.
func rewriteEvents(ctx context.Context, tx *sql.Tx, report func(done, total int64)) error {
	var total int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM events").Scan(&total); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT seq FROM events ORDER BY seq")
	if err != nil {
		return err
	}
	var seqs []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			return err
		}
		seqs = append(seqs, seq)
	}
	_ = rows.Close()
	for i, seq := range seqs {
		if _, err := tx.ExecContext(ctx, "UPDATE events SET test_col = 'v' || seq WHERE seq = ?", seq); err != nil {
			return err
		}
		report(int64(i+1), total)
	}
	return nil
}

func TestMigrateWithGoStep(t *testing.T) {
	s, p := openTestStore(t)
	fillStore(t, s, "a", 20)
	_ = s.Close()
	before := LatestSchemaVersion()
	v := withTestMigration(t, 1, rewriteEvents)
	lockDir := filepath.Join(filepath.Dir(p), "locks")

	// Every writer is refused until the migration ran; the data is intact.
	if _, err := Open(p, WithLockDir(lockDir)); !errors.Is(err, ErrSchemaOutdated) {
		t.Fatalf("Open = %v, want ErrSchemaOutdated", err)
	}
	ro, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, ro, "SELECT count(*) FROM events"); n != 20 || !ro.MigrationPending() {
		t.Errorf("events = %d, pending = %v", n, ro.MigrationPending())
	}
	plan, err := PlanMigration(ctx, p)
	if err != nil || plan.From != before || plan.To != v || len(plan.Steps) != 1 || plan.Steps[0].Space != 1 ||
		plan.NeedBytes != plan.SizeBytes*2+plan.SizeBytes/4 {
		t.Errorf("plan = %+v (%v)", plan, err)
	}

	var progress []Progress
	var logged []string
	res, err := Migrate(ctx, p, MigrateOptions{LockDir: lockDir,
		Logf:     func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) },
		Progress: func(pr Progress) { progress = append(progress, pr) }})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.Plan.From != before || res.Plan.To != v || res.Duration == 0 {
		t.Errorf("result = %+v", res)
	}
	if len(progress) != 21 || progress[0] != (Progress{Version: v}) || progress[20] != (Progress{Version: v, Done: 20, Total: 20}) {
		t.Errorf("progress = %+v", progress)
	}
	if j := strings.Join(logged, "\n"); !strings.Contains(j, PreMigrationBackup(p, before)) || !strings.Contains(j, fmt.Sprintf("migration v%d applied", v)) {
		t.Errorf("logged = %q", logged)
	}
	// The reader, still open, notices the migration on its next Poll.
	if c, err := ro.Poll(ctx); err != nil || c != Unchanged {
		t.Fatalf("first Poll = %v, %v", c, err)
	}
	// (The baseline was taken after the migration, so poll once more
	// after a further commit to see the version refresh path.)
	_ = ro.Close()
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("Open after Migrate: %v", err)
	}
	defer func() { _ = s2.Close() }()
	if n := countRows(t, s2, "SELECT count(*) FROM events WHERE test_col = 'v' || seq"); n != 20 {
		t.Errorf("rewritten events = %d, want 20", n)
	}
	if v2, _ := s2.SchemaVersion(ctx); v2 != v {
		t.Errorf("version = %d, want %d", v2, v)
	}
	if fi, err := os.Stat(p + "-wal"); err == nil && fi.Size() > 0 {
		t.Errorf("WAL not truncated after the migration: %d bytes", fi.Size())
	}
}

func TestPollRefreshesVersion(t *testing.T) {
	s, p := openTestStore(t)
	fillStore(t, s, "a", 3)
	_ = s.Close()
	withTestMigration(t, 0, nil)
	ro, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	if _, err := ro.Poll(ctx); err != nil { // baseline
		t.Fatal(err)
	}
	if !ro.MigrationPending() {
		t.Fatal("not pending before Migrate")
	}
	if _, err := Migrate(ctx, p, MigrateOptions{LockDir: filepath.Join(filepath.Dir(p), "locks")}); err != nil {
		t.Fatal(err)
	}
	if c, err := ro.Poll(ctx); err != nil || c != Modified {
		t.Fatalf("Poll after Migrate = %v, %v", c, err)
	}
	if ro.MigrationPending() {
		t.Error("still pending after the Poll that saw the migration")
	}
}

func TestMigrateFailingStepLeavesVersion(t *testing.T) {
	s, p := openTestStore(t)
	fillStore(t, s, "a", 3)
	_ = s.Close()
	before := LatestSchemaVersion()
	withTestMigration(t, 0, func(context.Context, *sql.Tx, func(int64, int64)) error { return errors.New("boom") })
	_, err := Migrate(ctx, p, MigrateOptions{LockDir: filepath.Join(filepath.Dir(p), "locks")})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Migrate = %v, want the step's error", err)
	}
	ro, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	if v, _ := ro.SchemaVersion(ctx); v != before {
		t.Errorf("version after failed step = %d, want %d", v, before)
	}
	if countRows(t, ro, "SELECT count(*) FROM pragma_table_info('events') WHERE name = 'test_col'") != 0 {
		t.Error("failed migration left its column")
	}
	if _, err := os.Stat(PreMigrationBackup(p, before)); err != nil {
		t.Errorf("the copy should stay after a failure: %v", err)
	}
}

func TestMigrateRefusals(t *testing.T) {
	newOutdated := func(t *testing.T) (string, string) {
		t.Helper()
		s, p := openTestStore(t)
		fillStore(t, s, "a", 3)
		_ = s.Close()
		return p, filepath.Join(filepath.Dir(p), "locks")
	}
	unchanged := func(t *testing.T, p string, want int) {
		t.Helper()
		ro, err := OpenReadOnly(p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ro.Close() }()
		if v, _ := ro.SchemaVersion(ctx); v != want {
			t.Errorf("version = %d, want %d (database was changed)", v, want)
		}
		if _, err := os.Stat(PreMigrationBackup(p, want)); err == nil {
			t.Error("a copy was written although the migration was refused")
		}
	}
	t.Run("no lock dir", func(t *testing.T) {
		p, _ := newOutdated(t)
		before := LatestSchemaVersion()
		withTestMigration(t, 0, nil)
		if _, err := Migrate(ctx, p, MigrateOptions{}); err == nil || !strings.Contains(err.Error(), "lock dir") {
			t.Errorf("Migrate = %v", err)
		}
		unchanged(t, p, before)
	})
	t.Run("writers active", func(t *testing.T) {
		p, lockDir := newOutdated(t)
		before := LatestSchemaVersion()
		w, err := Open(p, WithLockDir(lockDir)) // a hook still writing, opened before the new binary
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = w.Close() }()
		withTestMigration(t, 0, nil)
		if _, err := Migrate(ctx, p, MigrateOptions{LockDir: lockDir}); !errors.Is(err, ErrWritersActive) {
			t.Errorf("Migrate = %v, want ErrWritersActive", err)
		}
		unchanged(t, p, before)
	})
	t.Run("another maintenance run", func(t *testing.T) {
		p, lockDir := newOutdated(t)
		before := LatestSchemaVersion()
		release, err := lockMaintenance(lockDir, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		withTestMigration(t, 0, nil)
		if _, err := Migrate(ctx, p, MigrateOptions{LockDir: lockDir}); !errors.Is(err, ErrMaintenance) {
			t.Errorf("Migrate = %v, want ErrMaintenance", err)
		}
		unchanged(t, p, before)
	})
	t.Run("disk full", func(t *testing.T) {
		p, lockDir := newOutdated(t)
		before := LatestSchemaVersion()
		withTestMigration(t, 1, nil)
		saved := freeSpace
		freeSpace = func(string) (int64, error) { return 100, nil }
		t.Cleanup(func() { freeSpace = saved })
		_, err := Migrate(ctx, p, MigrateOptions{LockDir: lockDir})
		if !errors.Is(err, ErrDiskSpace) || !strings.Contains(err.Error(), "MiB needed") {
			t.Errorf("Migrate = %v, want ErrDiskSpace", err)
		}
		unchanged(t, p, before)
	})
	t.Run("missing database", func(t *testing.T) {
		if _, err := Migrate(ctx, filepath.Join(t.TempDir(), "none.db"), MigrateOptions{LockDir: t.TempDir()}); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Migrate on a missing file = %v", err)
		}
	})
}

func TestAllowOutdated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "archive.db")
	openAtVersion(t, p, 1)
	s, err := Open(p, AllowOutdated())
	if err != nil {
		t.Fatalf("Open with AllowOutdated: %v", err)
	}
	defer func() { _ = s.Close() }()
	if v, _ := s.SchemaVersion(ctx); v != 1 || !s.MigrationPending() {
		t.Errorf("version %d, pending %v: AllowOutdated must not migrate", v, s.MigrationPending())
	}
	// backup, the step before a migration, works on the outdated archive.
	dir := filepath.Join(t.TempDir(), "backups")
	bp, err := s.Backup(ctx, dir, time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC), 7, 0)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if _, err := os.Stat(bp); err != nil {
		t.Error(err)
	}
	if matches, _ := filepath.Glob(p + ".pre-v*.bak"); len(matches) != 0 {
		t.Errorf("AllowOutdated wrote a pre-migration copy: %v", matches)
	}
}

func TestCreateAtVersion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "archive.db")
	if err := CreateAtVersion(p, 0); err == nil {
		t.Error("version 0 accepted")
	}
	if err := CreateAtVersion(p, LatestSchemaVersion()+1); err == nil {
		t.Error("future version accepted")
	}
	if err := CreateAtVersion(p, 2); err != nil {
		t.Fatal(err)
	}
	if err := CreateAtVersion(p, 2); err == nil {
		t.Error("existing file accepted")
	}
	ro, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	if v, _ := ro.SchemaVersion(ctx); v != 2 {
		t.Errorf("version = %d", v)
	}
}
