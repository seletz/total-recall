package cli

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/skill"
	"github.com/seletz/total-recall/internal/store"
	tf "github.com/seletz/total-recall/internal/testfixture"
)

// testEnv is an isolated installation: config, DB, log, backups and a
// synthetic Claude projects tree in a temp dir.
type testEnv struct {
	dir, projects, db, log, backups, configPath string
	detached                                    [][]string
	now                                         time.Time
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	e := &testEnv{
		dir:        dir,
		projects:   tf.Demo(t, dir),
		db:         filepath.Join(dir, "data", "archive.db"),
		log:        filepath.Join(dir, "data", "total-recall.log"),
		backups:    filepath.Join(dir, "backups"),
		configPath: filepath.Join(dir, "config.toml"),
		now:        time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
	}
	cfg := fmt.Sprintf(`db = %q
timezone = "Europe/Berlin"

[backup]
dir = %q

[harness.claude]
projects_dir = %q

[git]
resolve = false
`, e.db, e.backups, e.projects)
	if err := os.WriteFile(e.configPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

type result struct {
	code           int
	stdout, stderr string
}

// run executes the CLI in-process.
func (e *testEnv) run(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	app := &App{
		Stdin:  strings.NewReader(stdin),
		Stdout: &out,
		Stderr: &errb,
		Args:   args,
		Home:   e.dir,
		Getenv: func(k string) string {
			if k == "TOTAL_RECALL_CONFIG" {
				return e.configPath
			}
			return ""
		},
		Now: func() time.Time { return e.now },
		StartDetached: func(args []string, logPath string) error {
			e.detached = append(e.detached, args)
			return nil
		},
	}
	code := Main(app)
	return result{code: code, stdout: out.String(), stderr: errb.String()}
}

func (e *testEnv) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	r := e.run(t, "", args...)
	if r.code != 0 {
		t.Fatalf("%v: exit %d\nstdout: %s\nstderr: %s", args, r.code, r.stdout, r.stderr)
	}
	return r.stdout
}

func (e *testEnv) count(t *testing.T, q string) int {
	t.Helper()
	if _, err := os.Stat(e.db); err != nil {
		return 0
	}
	st, err := store.OpenReadOnly(e.db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	_, rows, err := st.Query(t.Context(), q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return int(rows[0][0].(int64))
}

func (e *testEnv) logText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(e.log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func decode[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, s)
	}
	return v
}

// hookPayload loads testdata/hooks/<event>.json with the projects dir filled in.
func (e *testEnv) hookPayload(t *testing.T, event string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "hooks", event+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "{{PROJECTS}}", e.projects)
}

func TestHookContract(t *testing.T) {
	tests := []struct {
		name       string
		payload    func(e *testEnv, t *testing.T) string
		setup      func(e *testEnv, t *testing.T)
		wantEvents int
		wantSess   int
		wantError  string // substring of an ERROR log line; "" = no error logged
	}{
		{"SessionStart ingests its transcript and everything else",
			func(e *testEnv, t *testing.T) string { return e.hookPayload(t, "SessionStart") }, nil, 9, 3, ""},
		{"Stop ingests the session transcript",
			func(e *testEnv, t *testing.T) string { return e.hookPayload(t, "Stop") }, nil, 5, 1, ""},
		{"SubagentStop ingests session and subagent transcript",
			func(e *testEnv, t *testing.T) string { return e.hookPayload(t, "SubagentStop") }, nil, 7, 2, ""},
		{"SessionEnd ingests the session transcript",
			func(e *testEnv, t *testing.T) string { return e.hookPayload(t, "SessionEnd") }, nil, 2, 1, ""},
		{"missing transcript_path",
			func(e *testEnv, t *testing.T) string {
				return `{"session_id":"x","cwd":"/w","hook_event_name":"Stop","unknown":1}`
			}, nil, 0, 0, "no transcript_path"},
		{"empty transcript_path",
			func(e *testEnv, t *testing.T) string {
				return `{"session_id":"x","transcript_path":"","cwd":"/w","hook_event_name":"Stop"}`
			}, nil, 0, 0, "no transcript_path"},
		{"SessionStart without transcript_path still reads everything",
			func(e *testEnv, t *testing.T) string {
				return `{"session_id":"x","hook_event_name":"SessionStart","source":"resume"}`
			}, nil, 9, 3, ""},
		{"malformed JSON",
			func(e *testEnv, t *testing.T) string { return `{"session_id": "x", "transcript_path": ` }, nil, 0, 0, "invalid payload JSON"},
		{"empty stdin",
			func(e *testEnv, t *testing.T) string { return "" }, nil, 0, 0, "invalid payload JSON"},
		{"database from a newer binary",
			func(e *testEnv, t *testing.T) string { return e.hookPayload(t, "Stop") },
			func(e *testEnv, t *testing.T) {
				e.mustRun(t, "ingest", filepath.Join(e.projects, "-w-beta", tf.DemoSession2+".jsonl"))
				db, err := sql.Open("sqlite", e.db)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("UPDATE schema_version SET version = 999"); err != nil {
					t.Fatal(err)
				}
				_ = db.Close()
			}, -1, -1, "newer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			if tt.setup != nil {
				tt.setup(e, t)
			}
			r := e.run(t, tt.payload(e, t), "ingest", "--hook")
			if r.code != 0 {
				t.Fatalf("exit code %d, want 0 (stderr %q)", r.code, r.stderr)
			}
			if r.stdout != "" {
				t.Errorf("hook wrote to stdout (would reach the harness): %q", r.stdout)
			}
			log := e.logText(t)
			if tt.wantError == "" && strings.Contains(log, " ERROR ") {
				t.Errorf("unexpected error in log:\n%s", log)
			}
			if tt.wantError != "" && !strings.Contains(log, tt.wantError) {
				t.Errorf("log lacks %q:\n%s", tt.wantError, log)
			}
			if tt.wantEvents < 0 { // newer DB: nothing may have been written
				db, _ := sql.Open("sqlite", e.db)
				defer func() { _ = db.Close() }()
				var n int
				_ = db.QueryRow("SELECT count(*) FROM events").Scan(&n)
				if n != 2 {
					t.Errorf("events = %d, want the 2 from setup", n)
				}
				return
			}
			if got := e.count(t, "SELECT count(*) FROM events"); got != tt.wantEvents {
				t.Errorf("events = %d, want %d", got, tt.wantEvents)
			}
			if got := e.count(t, "SELECT count(*) FROM sessions"); got != tt.wantSess {
				t.Errorf("sessions = %d, want %d", got, tt.wantSess)
			}
		})
	}
}

// TestHookMissingTranscript covers #7: Claude Code fires hooks for files it
// has not written (yet) — SessionStart of a brand-new session, SubagentStop of
// internal agents without a transcript. That is expected, not an error: the
// hook logs it at INFO and still ingests whatever exists.
func TestHookMissingTranscript(t *testing.T) {
	tests := []struct {
		name       string
		payload    func(e *testEnv) string
		wantEvents int
		wantSess   int
		wantInfo   string
	}{
		{"Stop with missing transcript",
			func(e *testEnv) string {
				return `{"session_id":"x","transcript_path":"` + filepath.Join(e.projects, "gone.jsonl") + `","hook_event_name":"Stop"}`
			}, 0, 0, "gone.jsonl"},
		{"SubagentStop with missing agent transcript still ingests the session",
			func(e *testEnv) string {
				return `{"session_id":"` + tf.DemoSession1 + `","transcript_path":"` + filepath.Join(e.projects, "-w-alpha", tf.DemoSession1+".jsonl") +
					`","hook_event_name":"SubagentStop","agent_id":"a99","agent_transcript_path":"` +
					filepath.Join(e.projects, "-w-alpha", tf.DemoSession1, "subagents", "agent-a99.jsonl") + `"}`
			}, 5, 1, "agent-a99.jsonl"},
		{"SessionStart of a session not yet on disk still reads everything",
			func(e *testEnv) string {
				return `{"session_id":"new","transcript_path":"` + filepath.Join(e.projects, "-w-new", "new.jsonl") + `","hook_event_name":"SessionStart","source":"startup"}`
			}, 9, 3, "new.jsonl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			r := e.run(t, tt.payload(e), "ingest", "--hook")
			if r.code != 0 {
				t.Fatalf("exit code %d, want 0 (stderr %q)", r.code, r.stderr)
			}
			log := e.logText(t)
			if strings.Contains(log, " ERROR ") {
				t.Errorf("missing transcript logged as error:\n%s", log)
			}
			if !strings.Contains(log, " INFO ") || !strings.Contains(log, tt.wantInfo) {
				t.Errorf("log lacks INFO line for %q:\n%s", tt.wantInfo, log)
			}
			if got := e.count(t, "SELECT count(*) FROM events"); got != tt.wantEvents {
				t.Errorf("events = %d, want %d", got, tt.wantEvents)
			}
			if got := e.count(t, "SELECT count(*) FROM sessions"); got != tt.wantSess {
				t.Errorf("sessions = %d, want %d", got, tt.wantSess)
			}
		})
	}
}

func TestHookDetach(t *testing.T) {
	e := newTestEnv(t)
	payload := e.hookPayload(t, "Stop")
	r := e.run(t, payload, "--config", e.configPath, "ingest", "--hook", "--detach")
	if r.code != 0 || r.stdout != "" {
		t.Fatalf("result = %+v", r)
	}
	if len(e.detached) != 1 {
		t.Fatalf("detached starts = %v", e.detached)
	}
	args := e.detached[0]
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--detach ") || !strings.Contains(joined, "--hook") || !strings.Contains(joined, "--detached-child") {
		t.Errorf("child args = %v", args)
	}
	// The payload is handed over in a file that the child reads and deletes.
	var pf string
	for i, a := range args {
		if a == "--hook-payload" && i+1 < len(args) {
			pf = args[i+1]
		}
	}
	b, err := os.ReadFile(pf)
	if err != nil || string(b) != payload {
		t.Fatalf("payload file %q: %v", pf, err)
	}
	if got := e.count(t, "SELECT count(*) FROM events"); got != 0 {
		t.Errorf("parent ingested %d events", got)
	}
	// Run the child as the detached process would.
	r = e.run(t, "", args...)
	if r.code != 0 {
		t.Fatalf("child exit %d", r.code)
	}
	if got := e.count(t, "SELECT count(*) FROM events"); got != 5 {
		t.Errorf("child ingested %d events, want 5", got)
	}
	if _, err := os.Stat(pf); !os.IsNotExist(err) {
		t.Errorf("payload file not removed: %v", err)
	}
}

func TestHooksPrint(t *testing.T) {
	e := newTestEnv(t)
	type hook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type matcher struct {
		Matcher *string `json:"matcher"`
		Hooks   []hook  `json:"hooks"`
	}
	for _, bin := range []string{"", "/opt/bin/total-recall"} {
		args := []string{"hooks", "print", "--harness", "claude"}
		want := "total-recall"
		if bin != "" {
			args = append(args, "--binary", bin)
			want = bin
		}
		out := e.mustRun(t, args...)
		var raw map[string]map[string][]json.RawMessage
		if err := json.Unmarshal([]byte(out), &raw); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, out)
		}
		hooks := raw["hooks"]
		if len(raw) != 1 || len(hooks) != 4 {
			t.Fatalf("top level = %v", raw)
		}
		cmds := map[string][]string{}
		for _, ev := range []string{"SessionStart", "Stop", "SubagentStop", "SessionEnd"} {
			entries, ok := hooks[ev]
			if !ok || len(entries) == 0 {
				t.Fatalf("event %s missing", ev)
			}
			for _, rm := range entries {
				dec := json.NewDecoder(bytes.NewReader(rm))
				dec.DisallowUnknownFields()
				var m matcher
				if err := dec.Decode(&m); err != nil {
					t.Fatalf("%s entry not in settings.json shape: %v", ev, err)
				}
				for _, h := range m.Hooks {
					if h.Type != "command" || !strings.HasPrefix(h.Command, want+" ") {
						t.Errorf("%s hook = %+v", ev, h)
					}
					cmds[ev] = append(cmds[ev], strings.TrimPrefix(h.Command, want+" "))
				}
			}
		}
		for _, ev := range []string{"SessionStart", "Stop", "SubagentStop"} {
			if strings.Join(cmds[ev], ";") != "ingest --hook --detach" {
				t.Errorf("%s commands = %v", ev, cmds[ev])
			}
		}
		if strings.Join(cmds["SessionEnd"], ";") != "ingest --hook --detach;backup --if-older 24h --detach" {
			t.Errorf("SessionEnd commands = %v", cmds["SessionEnd"])
		}
	}
	if r := e.run(t, "", "hooks", "print", "--harness", "other"); r.code == 0 {
		t.Error("unknown harness accepted")
	}
}

func TestEndToEnd(t *testing.T) {
	e := newTestEnv(t)
	out := e.mustRun(t, "ingest", "--all")
	if !strings.Contains(out, "scanned 4 files") || !strings.Contains(out, "9 new events") {
		t.Errorf("ingest summary = %q", out)
	}
	if out := e.mustRun(t, "ingest", "--all"); !strings.Contains(out, "0 new events") || !strings.Contains(out, "4 unchanged") {
		t.Errorf("second ingest = %q", out)
	}

	sessions := decode[[]SessionJSON](t, e.mustRun(t, "sessions", "--json"))
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2 top-level", len(sessions))
	}
	byName := map[string]SessionJSON{}
	for _, s := range sessions {
		byName[s.DisplayName] = s
	}
	s1, s2 := byName["List files"], byName["Beta work"]
	if s1.HumanEvents != 2 || s1.AgentEvents != 3 || s1.NativeID != tf.DemoSession1 || s1.DurationSeconds != 5405 {
		t.Errorf("session 1 = %+v", s1)
	}
	if s2.HumanEvents != 1 || s2.AgentEvents != 1 || s2.Name != "Beta work" {
		t.Errorf("session 2 = %+v", s2)
	}
	if all := decode[[]SessionJSON](t, e.mustRun(t, "sessions", "--json", "--subagents")); len(all) != 3 {
		t.Errorf("with subagents = %d", len(all))
	}
	if f := decode[[]SessionJSON](t, e.mustRun(t, "sessions", "--json", "--since", "2026-10-06")); len(f) != 1 || f[0].DisplayName != "Beta work" {
		t.Errorf("--since = %+v", f)
	}
	if f := decode[[]SessionJSON](t, e.mustRun(t, "sessions", "--json", "--until", "2026-10-05")); len(f) != 1 || f[0].DisplayName != "List files" {
		t.Errorf("--until (whole day) = %+v", f)
	}
	table := e.mustRun(t, "sessions")
	for _, want := range []string{"START", "List files", "Beta work", "2026-10-05 10:00", "1h30m", tf.DemoSession1[:8]} {
		if !strings.Contains(table, want) {
			t.Errorf("sessions table lacks %q:\n%s", want, table)
		}
	}

	type showOut struct {
		Session SessionJSON `json:"session"`
		Events  []EventJSON `json:"events"`
	}
	sh := decode[showOut](t, e.mustRun(t, "show", tf.DemoSession1[:8], "--json"))
	if sh.Session.ID != "claude:"+tf.DemoSession1 || len(sh.Events) != 4 {
		t.Errorf("show = %+v", sh)
	}
	if all := decode[showOut](t, e.mustRun(t, "show", "claude:"+tf.DemoSession1, "--json", "--all")); len(all.Events) != 5 {
		t.Errorf("show --all events = %d", len(all.Events))
	}
	text := e.mustRun(t, "show", tf.DemoSession1)
	for _, want := range []string{"List files", "please list the files", "Forty-two lines.", "human", "agent"} {
		if !strings.Contains(text, want) {
			t.Errorf("show lacks %q:\n%s", want, text)
		}
	}
	if r := e.run(t, "", "show", "nope"); r.code == 0 || !strings.Contains(r.stderr, "no session") {
		t.Errorf("show unknown = %+v", r)
	}

	type statsOut struct {
		Total         ActivityJSON     `json:"total"`
		ByDay         []map[string]any `json:"by_day"`
		ByRepo        []map[string]any `json:"by_repo"`
		ByWeekdayHour []map[string]any `json:"by_weekday_hour"`
	}
	st := decode[statsOut](t, e.mustRun(t, "stats", "--month", "2026-10", "--json"))
	if st.Total.Human != 3 || st.Total.Agent != 5 || st.Total.Sessions != 3 {
		t.Errorf("stats total = %+v", st.Total)
	}
	if len(st.ByDay) != 2 || len(st.ByRepo) != 2 || len(st.ByWeekdayHour) != 7*24 {
		t.Errorf("stats shape: days %d repos %d cells %d", len(st.ByDay), len(st.ByRepo), len(st.ByWeekdayHour))
	}
	if empty := decode[statsOut](t, e.mustRun(t, "stats", "--month", "2026-09", "--json")); empty.Total.Human != 0 {
		t.Errorf("september = %+v", empty.Total)
	}
	if tbl := e.mustRun(t, "stats", "--since", "2026-10-01", "--until", "2026-10-31"); !strings.Contains(tbl, "2026-10-05") || !strings.Contains(tbl, "Mon") {
		t.Errorf("stats table:\n%s", tbl)
	}
}

func TestSearch(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	hits := decode[[]HitJSON](t, e.mustRun(t, "search", "release", "notes", "--json"))
	if len(hits) != 2 || hits[0].SessionName != "Beta work" {
		t.Errorf("hits = %+v", hits)
	}
	if h := decode[[]HitJSON](t, e.mustRun(t, "search", "release", "--json", "--actor", "human")); len(h) != 1 || h[0].Actor != "human" {
		t.Errorf("actor filter = %+v", h)
	}
	if h := decode[[]HitJSON](t, e.mustRun(t, "search", "--raw", "relea* OR forty", "--json")); len(h) != 3 {
		t.Errorf("raw = %d hits", len(h))
	}
	if out := e.mustRun(t, "search", "lines"); !strings.Contains(out, "[lines]") || !strings.Contains(out, "List files") {
		t.Errorf("table = %q", out)
	}
	// Characters that are FTS syntax are safe without --raw.
	e.mustRun(t, "search", `"unbalanced`, "AND", "--json")
}

func TestSQL(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	q := "SELECT harness, count(*) AS n FROM sessions GROUP BY harness"
	rows := decode[[]map[string]any](t, e.mustRun(t, "sql", "--json", q))
	if len(rows) != 1 || rows[0]["n"] != float64(3) {
		t.Errorf("json = %v", rows)
	}
	recs, err := csv.NewReader(strings.NewReader(e.mustRun(t, "sql", "--format", "csv", q))).ReadAll()
	if err != nil || len(recs) != 2 || recs[0][1] != "n" || recs[1][1] != "3" {
		t.Errorf("csv = %v %v", recs, err)
	}
	if out := e.mustRun(t, "sql", q); !strings.Contains(out, "harness") || !strings.Contains(out, "claude") {
		t.Errorf("table = %q", out)
	}
	if out := e.mustRun(t, "sql", "--json", "SELECT display_name FROM sessions WHERE native_id = '"+tf.DemoSession1+"'"); !strings.Contains(out, "List files") {
		t.Errorf("display_name = %s", out)
	}
	for _, w := range []string{"DELETE FROM sessions", "DROP TABLE events", "INSERT INTO dirs(cwd) VALUES ('x')"} {
		if r := e.run(t, "", "sql", w); r.code == 0 {
			t.Errorf("write %q succeeded", w)
		}
	}
	if got := e.count(t, "SELECT count(*) FROM sessions"); got != 3 {
		t.Errorf("sessions after write attempts = %d", got)
	}
}

func TestStatus(t *testing.T) {
	e := newTestEnv(t)
	if r := e.run(t, "", "status"); r.code == 0 || !strings.Contains(r.stderr, "ingest --all") {
		t.Errorf("status without DB = %+v", r)
	}
	e.mustRun(t, "ingest", "--all")
	newLogger(e.log, time.Now).Errorf("hook Stop: boom")
	s := decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
	if s.Sessions != 3 || s.Events != 9 || s.Sources != 4 || s.DB != e.db || s.DBSizeBytes == 0 {
		t.Errorf("status = %+v", s)
	}
	if s.SchemaVersion != store.LatestSchemaVersion() || s.BinarySchemaVersion != store.LatestSchemaVersion() {
		t.Errorf("schema versions = %d/%d", s.SchemaVersion, s.BinarySchemaVersion)
	}
	if s.LastIngest["claude"] == "" || len(s.RecentErrors) != 1 || !strings.Contains(s.RecentErrors[0], "boom") {
		t.Errorf("status = %+v", s)
	}
	out := e.mustRun(t, "status")
	for _, want := range []string{e.db, "schema", "last ingest (claude)", "none in", "boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}

func TestIngestErrorsExitNonZero(t *testing.T) {
	e := newTestEnv(t)
	if r := e.run(t, "", "ingest"); r.code == 0 {
		t.Error("ingest without arguments succeeded")
	}
	r := e.run(t, "", "ingest", filepath.Join(e.projects, "nope.jsonl"))
	if r.code == 0 || !strings.Contains(r.stderr, "nope.jsonl") {
		t.Errorf("result = %+v", r)
	}
}

func TestBackup(t *testing.T) {
	e := newTestEnv(t)
	if r := e.run(t, "", "backup"); r.code == 0 {
		t.Error("backup without DB succeeded")
	}
	e.mustRun(t, "ingest", "--all")
	out := e.mustRun(t, "backup")
	want := filepath.Join(e.backups, "archive-2026-10-20.db.gz")
	if !strings.Contains(out, want) {
		t.Errorf("backup = %q", out)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
	// The date uses the configured time zone: 23:30 UTC is the next day in Berlin.
	e.now = time.Date(2026, 10, 21, 23, 30, 0, 0, time.UTC)
	if out := e.mustRun(t, "backup"); !strings.Contains(out, "archive-2026-10-22.db.gz") {
		t.Errorf("tz date = %q", out)
	}
	// --if-older compares with the newest copy's modification time (real clock).
	e.now = time.Now()
	if out := e.mustRun(t, "backup", "--if-older", "24h"); !strings.Contains(out, "skipped") {
		t.Errorf("--if-older 24h = %q", out)
	}
	e.now = time.Now().Add(48 * time.Hour)
	if out := e.mustRun(t, "backup", "--if-older", "24h"); !strings.Contains(out, "backup written") {
		t.Errorf("--if-older after 48h = %q", out)
	}
	if r := e.run(t, "", "backup", "--detach", "--if-older", "24h"); r.code != 0 || len(e.detached) != 1 {
		t.Errorf("detach = %+v %v", r, e.detached)
	}
	if r := e.run(t, "", "backup", "--if-older", "soon"); r.code == 0 {
		t.Error("invalid duration accepted")
	}
}

func TestRestore(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	e.mustRun(t, "backup")
	bak := filepath.Join(e.backups, "archive-2026-10-20.db.gz")
	events := e.count(t, "SELECT count(*) FROM events")

	// Default target is the configured database, which exists: refused.
	if r := e.run(t, "", "restore", bak); r.code == 0 || !strings.Contains(r.stderr, "exists") {
		t.Errorf("restore over live DB = %+v", r)
	}
	to := filepath.Join(e.dir, "restored", "archive.db")
	out := e.mustRun(t, "restore", bak, "--to", to)
	if !strings.Contains(out, to) {
		t.Errorf("restore output = %q", out)
	}
	n := e.mustRun(t, "--db", to, "sql", "SELECT count(*) AS n FROM events", "--format", "csv")
	if !strings.Contains(n, fmt.Sprint(events)) {
		t.Errorf("restored events = %q, want %d", n, events)
	}
	if r := e.run(t, "", "restore", bak, "--to", to); r.code == 0 {
		t.Error("second restore to the same path succeeded without --force")
	}
	e.mustRun(t, "restore", bak, "--to", to, "--force")
	if r := e.run(t, "", "restore"); r.code == 0 {
		t.Error("restore without argument succeeded")
	}
}

func TestSessionsHidesEmpty(t *testing.T) {
	e := newTestEnv(t)
	tf.WriteFile(t, e.projects, "-w-delta/44444444-4444-4444-8444-444444444444.jsonl",
		tf.CustomTitle("44444444-4444-4444-8444-444444444444", "title only").JSON())
	e.mustRun(t, "ingest", "--all")
	list := decode[[]SessionJSON](t, e.mustRun(t, "sessions", "--json"))
	for _, s := range list {
		if s.DisplayName == "title only" {
			t.Errorf("empty session listed: %+v", s)
		}
	}
	all := decode[[]SessionJSON](t, e.mustRun(t, "sessions", "--json", "--all-sessions"))
	if len(all) != len(list)+1 {
		t.Errorf("--all-sessions: %d sessions, want %d", len(all), len(list)+1)
	}
}

func TestIngestRegit(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	if r := e.run(t, "", "ingest", "--regit"); r.code == 0 || !strings.Contains(r.stderr, "git.resolve") {
		t.Errorf("regit with git.resolve=false = %+v", r)
	}
	b, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configPath, []byte(strings.Replace(string(b), "resolve = false", "resolve = true", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.mustRun(t, "ingest", "--regit")
	if !strings.Contains(out, "regit: 2 directories") || strings.Contains(out, "scanned") {
		t.Errorf("regit output = %q", out)
	}
	if out := e.mustRun(t, "ingest", "--all", "--regit"); !strings.Contains(out, "scanned") || !strings.Contains(out, "regit:") {
		t.Errorf("ingest --all --regit output = %q", out)
	}
}

func TestDoctor(t *testing.T) {
	e := newTestEnv(t)
	r := e.run(t, "", "doctor")
	if r.code != 0 || !strings.Contains(r.stdout, "no unknown") {
		t.Fatalf("doctor on clean fixtures = %+v", r)
	}
	if _, err := os.Stat(e.db); err == nil {
		t.Error("doctor created the database")
	}
	tf.WriteFile(t, e.projects, "-w-gamma/33333333-3333-4333-8333-333333333333.jsonl", tf.Join(
		tf.Line{"type": "brand-new", "sessionId": "x"},
		tf.Line{"type": "assistant", "uuid": "d1", "timestamp": "2026-10-07T10:00:00Z", "sessionId": "x", "cwd": "/w",
			"version": "2.1.999", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "hologram"}}}},
	))
	r = e.run(t, "", "doctor", "--json")
	if r.code != 2 {
		t.Fatalf("doctor with drift: exit %d, want 2\n%s%s", r.code, r.stdout, r.stderr)
	}
	reps := decode[[]harness.FormatReport](t, r.stdout)
	if len(reps) != 1 || reps[0].UnknownRecordTypes["brand-new"] == nil || reps[0].UnknownBlockTypes["assistant/hologram"] == nil {
		t.Errorf("report = %+v", reps)
	}
	if len(reps[0].Versions) == 0 || reps[0].Versions[0].Version != "2.1.999" {
		t.Errorf("versions = %+v", reps[0].Versions)
	}
	r = e.run(t, "", "doctor")
	if r.code != 2 || !strings.Contains(r.stdout, "brand-new") || !strings.Contains(r.stdout, "assistant/hologram") {
		t.Errorf("doctor text = %+v", r)
	}
	if r := e.run(t, "", "doctor", "--harness", "nope"); r.code != 1 {
		t.Errorf("unknown harness: %+v", r)
	}
}

func TestConfigCommands(t *testing.T) {
	e := newTestEnv(t)
	c := decode[ConfigJSON](t, e.mustRun(t, "config", "show", "--json"))
	if !c.Loaded || c.File != e.configPath || c.FileOrigin != "env" {
		t.Errorf("config = %+v", c)
	}
	origins := map[string]string{}
	for _, v := range c.Values {
		origins[v.Key] = v.Origin + "=" + v.Value
	}
	if origins["db"] != "file="+e.db || !strings.HasPrefix(origins["redact.enabled"], "default=") {
		t.Errorf("origins = %v", origins)
	}
	if v := decode[ConfigJSON](t, e.mustRun(t, "--db", "/x/y.db", "config", "show", "--json")); v.Values[0].Origin != "flag" {
		t.Errorf("--db origin = %+v", v.Values[0])
	}
	if out := e.mustRun(t, "config", "show"); !strings.Contains(out, "ORIGIN") {
		t.Errorf("table = %q", out)
	}

	p := filepath.Join(e.dir, "new", "config.toml")
	if out := e.mustRun(t, "--config", p, "config", "init"); !strings.Contains(out, p) {
		t.Errorf("init = %q", out)
	}
	if r := e.run(t, "", "--config", p, "config", "init"); r.code == 0 || !strings.Contains(r.stderr, "not overwriting") {
		t.Errorf("second init = %+v", r)
	}
}

func TestLLMSkillAndVersion(t *testing.T) {
	e := newTestEnv(t)
	if out := e.mustRun(t, "llm", "--skill"); out != skill.Text() {
		t.Error("llm --skill differs from the embedded skill")
	}
	if r := e.run(t, "", "llm"); r.code == 0 {
		t.Error("llm without --skill succeeded")
	}
	if out := e.mustRun(t, "version"); strings.TrimSpace(out) == "" {
		t.Error("empty version")
	}
	if out := e.mustRun(t, "completion", "zsh"); !strings.Contains(out, "total-recall") {
		t.Error("completion command missing")
	}
}

func TestReadOnlyOnOlderSchemaWarns(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	db, err := sql.Open("sqlite", e.db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE schema_version SET version = 1"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	r := e.run(t, "", "sql", "SELECT count(*) FROM sessions")
	if r.code != 0 || !strings.Contains(r.stderr, "older") {
		t.Errorf("result = %+v", r)
	}
}

func TestChildArgs(t *testing.T) {
	got := childArgs([]string{"--db", "x", "ingest", "--hook", "--detach"}, "--hook-payload", "/tmp/p")
	want := "--db x ingest --hook --detached-child --hook-payload /tmp/p"
	if strings.Join(got, " ") != want {
		t.Errorf("childArgs = %v", got)
	}
}
