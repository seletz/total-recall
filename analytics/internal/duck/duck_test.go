package duck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), Options{ExtDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestLockMatchesLinkedLibrary(t *testing.T) {
	lock, err := ParseLock(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	db := openTest(t)
	info, err := db.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.DuckDBVersion != lock.DuckDB {
		t.Errorf("linked DuckDB %s, ext.lock pins %s: run scripts/analytics-ext.sh --update-lock %s", info.DuckDBVersion, lock.DuckDB, info.DuckDBVersion)
	}
	if info.Platform != Platform {
		t.Errorf("DuckDB platform %q, embedded extension built for %q", info.Platform, Platform)
	}
	if _, ok := lock.Find(SQLiteExtension, Platform); !ok {
		t.Errorf("ext.lock has no %s for %s", SQLiteExtension, Platform)
	}
}

func TestEmbeddedExtensionMatchesLock(t *testing.T) {
	lock, err := ParseLock(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := lock.Find(SQLiteExtension, Platform)
	sum := sha256.Sum256(sqliteExtGz)
	if got := hex.EncodeToString(sum[:]); got != e.GzSHA256 {
		t.Errorf("embedded .gz sha256 %s, lock %s", got, e.GzSHA256)
	}
}

func TestSettingsKeepDuckDBOffline(t *testing.T) {
	db := openTest(t)
	for _, tc := range []struct{ name, want string }{
		{"autoinstall_known_extensions", "false"},
		{"autoload_known_extensions", "false"},
		{"allow_community_extensions", "false"},
		{"allow_unsigned_extensions", "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			if err := db.QueryRowContext(context.Background(), "SELECT current_setting('"+tc.name+"')::VARCHAR").Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("%s = %s, want %s", tc.name, got, tc.want)
			}
		})
	}
}

func TestAttachSQLiteReadOnly(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	path := filepath.Join(t.TempDir(), "host-a.db")
	// Write a small SQLite file through the extension itself.
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

func TestMaterializeExtension(t *testing.T) {
	lock, err := ParseLock(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := lock.Find(SQLiteExtension, Platform)
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{"missing", func(t *testing.T, path string) {}},
		{"corrupt", func(t *testing.T, path string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("not an extension"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"present", func(t *testing.T, path string) {
			if _, err := materialize(filepath.Dir(path), e); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			want := filepath.Join(dir, SQLiteExtension+".duckdb_extension")
			tc.setup(t, want)
			got, err := materialize(dir, e)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("path %s, want %s", got, want)
			}
			b, err := os.ReadFile(got)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(b)
			if hex.EncodeToString(sum[:]) != e.SHA256 {
				t.Error("materialized extension does not match the lock")
			}
		})
	}
}

func TestParseLock(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		ok       bool
	}{
		{"valid", "# c\nduckdb v1.2.3\nsqlite_scanner osx_arm64 aa bb\n", true},
		{"no version", "sqlite_scanner osx_arm64 aa bb\n", false},
		{"short line", "duckdb v1\nsqlite_scanner osx_arm64 aa\n", false},
		{"unknown key", "duckdb v1\nfoo\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseLock([]byte(tc.in))
			if (err == nil) != tc.ok {
				t.Errorf("err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestInfoReportsEmbeddedExtension(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(context.Background(), Options{ExtDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	info, err := db.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []Extension
	for _, e := range info.Extensions {
		if e.Name == SQLiteExtension {
			got = append(got, e)
		}
	}
	want := filepath.Join(dir, SQLiteExtension+".duckdb_extension")
	if len(got) != 1 || !got[0].Embedded || got[0].Path != want || got[0].Version == "" {
		t.Errorf("extensions = %+v, want embedded %s at %s", info.Extensions, SQLiteExtension, want)
	}
	var extDir string
	if err := db.QueryRowContext(context.Background(), "SELECT current_setting('extension_directory')").Scan(&extDir); err != nil {
		t.Fatal(err)
	}
	if extDir != dir {
		t.Errorf("extension_directory = %s, want %s", extDir, dir)
	}
}
