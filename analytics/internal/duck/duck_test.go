package duck

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// lockVersion reads version and commit from analytics/duckdb/duckdb.lock.
func lockVersion(t *testing.T) (version, commit string) {
	t.Helper()
	f, err := os.Open("../../duckdb/duckdb.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fs := strings.Fields(sc.Text()); len(fs) == 3 && fs[0] == "duckdb" {
			return fs[1], fs[2]
		}
	}
	t.Fatal("no duckdb line in duckdb.lock")
	return "", ""
}

func TestLinkedDuckDBMatchesLock(t *testing.T) {
	version, commit := lockVersion(t)
	info, err := openTest(t).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.DuckDBVersion != version {
		t.Errorf("linked DuckDB %s, duckdb.lock pins %s (rebuild with scripts/analytics-duckdb.sh)", info.DuckDBVersion, version)
	}
	if !strings.HasPrefix(commit, info.SourceID) {
		t.Errorf("linked DuckDB source %s, duckdb.lock pins %s", info.SourceID, commit)
	}
}

func TestExtensionsAreCompiledIn(t *testing.T) {
	info, err := openTest(t).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range info.Extensions {
		if !e.Static {
			t.Errorf("extension %s is loaded but not compiled in", e.Name)
		}
		got[e.Name] = true
	}
	for _, want := range []string{SQLiteExtension, "parquet", "json", "icu", "core_functions"} {
		if !got[want] {
			t.Errorf("extension %s missing; loaded: %+v", want, info.Extensions)
		}
	}
}

// TestNoExtensionLoading: the build cannot load or install extensions.
func TestNoExtensionLoading(t *testing.T) {
	db := openTest(t)
	for _, q := range []string{"INSTALL httpfs", "LOAD httpfs", "LOAD '/tmp/x.duckdb_extension'"} {
		if _, err := db.Exec(q); err == nil {
			t.Errorf("%s succeeded", q)
		}
	}
}

func TestSettingsKeepDuckDBOffline(t *testing.T) {
	db := openTest(t)
	for _, s := range settings {
		t.Run(s[0], func(t *testing.T) {
			var got string
			if err := db.QueryRowContext(context.Background(), "SELECT current_setting('"+s[0]+"')::VARCHAR").Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != s[1] {
				t.Errorf("%s = %s, want %s", s[0], got, s[1])
			}
		})
	}
}

func TestAttachSQLiteReadOnly(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	path := filepath.Join(t.TempDir(), "host-a.db")
	for _, q := range []string{
		"ATTACH '" + path + "' AS w (TYPE sqlite)",
		"CREATE TABLE w.events (id INTEGER, kind TEXT)",
		"INSERT INTO w.events VALUES (1, 'message'), (2, 'tool_use'), (3, 'tool_result')",
		"DETACH w",
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := db.AttachSQLite(ctx, path, "host_a"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM host_a.events").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("events = %d, want 3", n)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO host_a.events VALUES (4, 'message')"); err == nil {
		t.Error("insert into a read-only attached archive succeeded")
	}
}

func TestAttachSQLiteRejectsBadAlias(t *testing.T) {
	db := openTest(t)
	for _, alias := range []string{"", "a b", "x;DROP", "1abc", `a"b`} {
		if err := db.AttachSQLite(context.Background(), "/nonexistent.db", alias); err == nil {
			t.Errorf("alias %q accepted", alias)
		}
	}
}
