package star

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/seletz/total-recall/analytics/internal/duck"
	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

var update = flag.Bool("update", false, "rewrite project/contract.txt from the current views")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(fixturedb.DemoMain(m))
}

// openDuck opens (or creates) a DuckDB analytics database.
func openDuck(t *testing.T, path string) *duck.DB {
	t.Helper()
	db, err := duck.Open(context.Background(), duck.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// demoCopy copies the demo archive into the test's temp dir.
func demoCopy(t *testing.T) string {
	t.Helper()
	in, err := os.Open(os.Getenv(fixturedb.DemoEnv))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	dst := filepath.Join(t.TempDir(), "archive.db")
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return dst
}

// execSQLite changes an archive outside the ETL.
func execSQLite(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func opts(t *testing.T, archive string) Options {
	t.Helper()
	return Options{Archive: archive, Source: "host-a", Host: "host-a", TZ: berlin(t), Home: demo.Home, Worktime: defaultWorktime(t)}
}

// defaultWorktime is [worktime] of a config without the section, as
// stats --worktime uses it.
func defaultWorktime(t *testing.T) *Worktime {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("timezone = \"Europe/Berlin\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{ConfigFile: path, Home: demo.Home, Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	ws, err := cfg.WorktimeSettings()
	if err != nil {
		t.Fatal(err)
	}
	return &Worktime{Settings: ws, DayStart: cfg.DayStartOffset()}
}

func mustRun(t *testing.T, db *duck.DB, o Options) Report {
	t.Helper()
	r, err := Run(context.Background(), db, o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return r
}

// row returns the first row of a query as "a|b|c".
func row(t *testing.T, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, q string, args ...any) string {
	t.Helper()
	rows := table(t, db, q, args...)
	if len(rows) == 0 {
		return ""
	}
	return rows[0]
}

// table returns all rows of a query as "a|b|c" strings.
func table(t *testing.T, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, q string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var out []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			switch x := v.(type) {
			case nil:
				parts[i] = ""
			case []byte:
				parts[i] = string(x)
			case time.Time:
				parts[i] = x.Format("2006-01-02")
			default:
				parts[i] = fmt.Sprint(x)
			}
		}
		out = append(out, strings.Join(parts, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b []string
		same bool
	}{
		{"deterministic", []string{"model", "m1"}, []string{"model", "m1"}, true},
		{"dimension separates", []string{"model", "x"}, []string{"tool", "x"}, false},
		{"parts are not concatenated", []string{"harness", "ab", "c"}, []string{"harness", "a", "bc"}, false},
		{"empty natural key is a member", []string{"model", ""}, []string{"model"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ka, kb := Key(tc.a...), Key(tc.b...)
			if (ka == kb) != tc.same || ka == 0 {
				t.Errorf("Key(%q)=%d, Key(%q)=%d, want same=%v", tc.a, ka, tc.b, kb, tc.same)
			}
		})
	}
}

// TestKeyMacroMatchesGoAndPostgres: the SQL macro, the Go function and the
// PostgreSQL expression give the same keys, so every engine and run agrees.
func TestKeyMacroMatchesGoAndPostgres(t *testing.T) {
	const pg = int64(-2923758452680610497) // postgres:17-alpine, spike #349
	if got := Key("model", "m1"); got != pg {
		t.Errorf("Key(model, m1) = %d, want %d", got, pg)
	}
	db := openDuck(t, "")
	for _, s := range splitSQL(mustRead(t, "macros.sql")) {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	for _, parts := range [][]string{
		{"model", "m1"}, {"model", ""}, {"host", "host-a"}, {"project", "~/src/kompass"},
		{"harness", "claude", "2.1.0"}, {"event", "e-0001"}, {"tool", "mcp__kompass__route"},
	} {
		args := make([]string, len(parts))
		for i, p := range parts {
			args[i] = "'" + sqlText(p) + "'"
		}
		var got int64
		if err := db.QueryRow("SELECT tr_key(concat_ws(chr(31), " + strings.Join(args, ", ") + "))").Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := Key(parts...); got != want {
			t.Errorf("tr_key(%q) = %d, Key = %d", parts, got, want)
		}
	}
}

func TestTildeMacroMatchesGo(t *testing.T) {
	db := openDuck(t, "")
	for _, s := range splitSQL(mustRead(t, "macros.sql")) {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ path, home, want string }{
		{"/home/mara/src/kompass", "/home/mara", "~/src/kompass"},
		{"/home/mara", "/home/mara", "~"},
		{"/home/mara/x", "/home/mara/", "~/x"},
		{"/home/marabou/x", "/home/mara", "/home/marabou/x"},
		{"/srv/wetterwarte", "/home/mara", "/srv/wetterwarte"},
		{"/home/mara/x", "", "/home/mara/x"},
		{"", "/home/mara", ""},
	} {
		var got string
		if err := db.QueryRow("SELECT tr_tilde(?, ?)", tc.path, tc.home).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != tc.want || Tilde(tc.path, tc.home) != tc.want {
			t.Errorf("tr_tilde(%q, %q) = %q, Tilde = %q, want %q", tc.path, tc.home, got, Tilde(tc.path, tc.home), tc.want)
		}
		if tc.home != "" && tc.want != "" && untilde(tc.want, tc.home) != tc.path && tc.home[len(tc.home)-1] != '/' {
			t.Errorf("untilde(%q) = %q", tc.want, untilde(tc.want, tc.home))
		}
	}
}

func mustRead(t *testing.T, name string) string {
	t.Helper()
	b, err := readProject(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestContract builds the demo and checks the views against contract.txt
// (Run fails on a difference); -update rewrites the file after a deliberate
// change.
func TestContract(t *testing.T) {
	db := openDuck(t, filepath.Join(t.TempDir(), "a.duckdb"))
	skipContract = *update
	defer func() { skipContract = false }()
	r := mustRun(t, db, opts(t, demoCopy(t)))
	if *update {
		got, err := DescribeViews(context.Background(), db)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("project/contract.txt", []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if r.Tests == 0 || r.Models == 0 || r.Events == 0 {
		t.Errorf("report %+v", r)
	}
}
