package cli

import (
	"bytes"
	"cmp"
	"database/sql"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/logfile"
	"github.com/seletz/total-recall/internal/skill"
	"github.com/seletz/total-recall/internal/store"
	tf "github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/tui"
	"github.com/seletz/total-recall/internal/version"
)

// testEnv is an isolated installation: config, DB, log, backups and a
// synthetic Claude projects tree in a temp dir.
type testEnv struct {
	dir, projects, db, log, backups, configPath string
	detached                                    [][]string
	now                                         time.Time
	home                                        string // App.Home; empty means dir
	width                                       int    // App.TerminalWidth; 0 = stdout is no terminal
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	e := &testEnv{
		dir:        dir,
		projects:   tf.Small(t, dir),
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
		Home:   cmp.Or(e.home, e.dir),
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
		TerminalWidth: func() int { return e.width },
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
			func(e *testEnv, t *testing.T) string { return e.hookPayload(t, "SessionStart") }, nil, 9, 4, ""},
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
			}, nil, 9, 4, ""},
		{"malformed JSON",
			func(e *testEnv, t *testing.T) string { return `{"session_id": "x", "transcript_path": ` }, nil, 0, 0, "invalid payload JSON"},
		{"empty stdin",
			func(e *testEnv, t *testing.T) string { return "" }, nil, 0, 0, "invalid payload JSON"},
		{"database from a newer binary",
			func(e *testEnv, t *testing.T) string { return e.hookPayload(t, "Stop") },
			func(e *testEnv, t *testing.T) {
				e.mustRun(t, "ingest", filepath.Join(e.projects, "-w-beta", tf.SmallSession2+".jsonl"))
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
				return `{"session_id":"` + tf.SmallSession1 + `","transcript_path":"` + filepath.Join(e.projects, "-w-alpha", tf.SmallSession1+".jsonl") +
					`","hook_event_name":"SubagentStop","agent_id":"a99","agent_transcript_path":"` +
					filepath.Join(e.projects, "-w-alpha", tf.SmallSession1, "subagents", "agent-a99.jsonl") + `"}`
			}, 5, 1, "agent-a99.jsonl"},
		{"SessionStart of a session not yet on disk still reads everything",
			func(e *testEnv) string {
				return `{"session_id":"new","transcript_path":"` + filepath.Join(e.projects, "-w-new", "new.jsonl") + `","hook_event_name":"SessionStart","source":"startup"}`
			}, 9, 4, "new.jsonl"},
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
	// The workflow journal is the one meta record of the small tree (#201).
	if !strings.Contains(out, "scanned 4 files") || !strings.Contains(out, "9 new events, 1 meta records") {
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
		byName[string(s.DisplayName)] = s
	}
	s1, s2 := byName["List files"], byName["Beta work"]
	if s1.HumanEvents != 2 || s1.AgentEvents != 3 || s1.NativeID != tf.SmallSession1 || s1.DurationSeconds != 5405 {
		t.Errorf("session 1 = %+v", s1)
	}
	if s2.HumanEvents != 1 || s2.AgentEvents != 1 || s2.Name != "Beta work" {
		t.Errorf("session 2 = %+v", s2)
	}
	// Session 1 carries session-owner records, session 2 does not; no
	// [accounts] are configured, so the name is null.
	if s1.AccountID != NullString(tf.Account1) || s1.OrganizationID != NullString(tf.Organization1) || s1.Account != "" || s1.ContinuedIn != "" || s1.ContinuedFrom != "" {
		t.Errorf("session 1 account/links = %+v", s1)
	}
	if s2.AccountID != "" || s2.OrganizationID != "" || s2.Account != "" {
		t.Errorf("session 2 account = %+v", s2)
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
	for _, want := range []string{"START", "List files", "Beta work", "2026-10-05 10:00", "1h30m", tf.SmallSession1[:8]} {
		if !strings.Contains(table, want) {
			t.Errorf("sessions table lacks %q:\n%s", want, table)
		}
	}

	type showOut struct {
		Session SessionJSON `json:"session"`
		Events  []EventJSON `json:"events"`
	}
	sh := decode[showOut](t, e.mustRun(t, "show", tf.SmallSession1[:8], "--json"))
	if sh.Session.ID != "claude:"+tf.SmallSession1 || len(sh.Events) != 4 {
		t.Errorf("show = %+v", sh)
	}
	if all := decode[showOut](t, e.mustRun(t, "show", "claude:"+tf.SmallSession1, "--json", "--all")); len(all.Events) != 5 {
		t.Errorf("show --all events = %d", len(all.Events))
	}
	text := e.mustRun(t, "show", tf.SmallSession1)
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

func TestSearchBySession(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	sub := "claude:agent-a01"
	main := "claude:" + tf.SmallSession1
	type row struct {
		id    string
		hits  int
		first string
	}
	tests := []struct {
		name string
		args []string
		want []row
	}{
		// "count" occurs in the main session (09:30) and in its subagent (08:00:10).
		{"timeline includes subagents", []string{"count"}, []row{{sub, 1, "2026-10-05T08:00:10.000Z"}, {main, 1, "2026-10-05T09:30:00.000Z"}}},
		{"order hits", []string{"--raw", "lines OR count", "--order", "hits"}, []row{{main, 2, "2026-10-05T09:30:00.000Z"}, {sub, 1, "2026-10-05T08:00:10.000Z"}}},
		{"actor", []string{"count", "--actor", "human"}, []row{{main, 1, "2026-10-05T09:30:00.000Z"}}},
		{"since", []string{"count", "--since", "2026-10-05T11:00"}, []row{{main, 1, "2026-10-05T09:30:00.000Z"}}}, // local time, UTC+2
		{"repo", []string{"count", "--repo", "beta"}, nil},
		// Two sessions match; the limit keeps the one with the latest hit, not the oldest.
		{"limit keeps most recent", []string{"count", "--limit", "1"}, []row{{main, 1, "2026-10-05T09:30:00.000Z"}}},
		{"limit by hits", []string{"--raw", "lines OR count", "--order", "hits", "--limit", "1"}, []row{{main, 2, "2026-10-05T09:30:00.000Z"}}},
		{"session", []string{"count", "--session", tf.SmallSession1[:8]}, []row{{main, 1, "2026-10-05T09:30:00.000Z"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"search", "--by-session", "--json"}, tt.args...)
			got := decode[[]SessionHitsJSON](t, e.mustRun(t, args...))
			if len(got) != len(tt.want) {
				t.Fatalf("got %d rows, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				g := got[i]
				if g.SessionID != w.id || g.Hits != w.hits || g.FirstHit != w.first {
					t.Errorf("row %d = %+v, want %+v", i, g, w)
				}
				if g.DisplayName == "" || g.Repo == "" || g.Snippet == "" || g.LastHit == "" {
					t.Errorf("row %d has empty fields: %+v", i, g)
				}
			}
		})
	}
	out := e.mustRun(t, "search", "count", "--by-session")
	if !strings.Contains(out, "FIRST") || !strings.Contains(out, "List files") || !strings.Contains(out, "[count]") || !strings.Contains(out, "2026-10-05 11:30") {
		t.Errorf("table = %q", out)
	}
	if r := e.run(t, "", "search", "count", "--by-session", "--order", "rank"); r.code == 0 || !strings.Contains(r.stderr, "--order") {
		t.Errorf("bad order: code %d, stderr %q", r.code, r.stderr)
	}
	if r := e.run(t, "", "search", "count", "--order", "hits"); r.code == 0 || !strings.Contains(r.stderr, "--by-session") {
		t.Errorf("order without by-session: code %d, stderr %q", r.code, r.stderr)
	}
}

func TestSQL(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	q := "SELECT harness, count(*) AS n FROM sessions GROUP BY harness"
	rows := decode[[]map[string]any](t, e.mustRun(t, "sql", "--json", q))
	// Four sessions: two main ones, a subagent and the workflow agent whose
	// journal (a meta record) names it.
	if len(rows) != 1 || rows[0]["n"] != float64(4) {
		t.Errorf("json = %v", rows)
	}
	recs, err := csv.NewReader(strings.NewReader(e.mustRun(t, "sql", "--format", "csv", q))).ReadAll()
	if err != nil || len(recs) != 2 || recs[0][1] != "n" || recs[1][1] != "4" {
		t.Errorf("csv = %v %v", recs, err)
	}
	if out := e.mustRun(t, "sql", q); !strings.Contains(out, "harness") || !strings.Contains(out, "claude") {
		t.Errorf("table = %q", out)
	}
	if out := e.mustRun(t, "sql", "--json", "SELECT display_name FROM sessions WHERE native_id = '"+tf.SmallSession1+"'"); !strings.Contains(out, "List files") {
		t.Errorf("display_name = %s", out)
	}
	for _, w := range []string{"DELETE FROM sessions", "DROP TABLE events", "INSERT INTO dirs(cwd) VALUES ('x')"} {
		if r := e.run(t, "", "sql", w); r.code == 0 {
			t.Errorf("write %q succeeded", w)
		}
	}
	if got := e.count(t, "SELECT count(*) FROM sessions"); got != 4 {
		t.Errorf("sessions after write attempts = %d", got)
	}
}

func TestStatus(t *testing.T) {
	e := newTestEnv(t)
	if r := e.run(t, "", "status"); r.code == 0 || !strings.Contains(r.stderr, "ingest --all") {
		t.Errorf("status without DB = %+v", r)
	}
	e.mustRun(t, "ingest", "--all")
	// Three days ago: an old error and a missing-transcript error, which
	// never counts. Three hours ago: a fresh error.
	at := func(d time.Duration) func() time.Time { return func() time.Time { return e.now.Add(d) } }
	logfile.New(e.log, at(-72*time.Hour)).Errorf("hook Stop: /p/a.jsonl: permission denied")
	logfile.New(e.log, at(-71*time.Hour)).Errorf("hook SubagentStop: /p/b.jsonl: stat /p/b.jsonl: no such file or directory")
	logfile.New(e.log, at(-3*time.Hour)).Infof("hook SubagentStop: transcript not present, skipped: /p/c.jsonl")
	logfile.New(e.log, at(-3*time.Hour)).Errorf("hook Stop: boom")
	s := decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
	if s.Sessions != 4 || s.Events != 9 || s.Sources != 4 || s.DB != e.db || s.DBSizeBytes == 0 {
		t.Errorf("status = %+v", s)
	}
	// The Small fixture has one meta record: the workflow journal's started record.
	if s.MetaRows != 1 || s.MetaBytes <= 0 {
		t.Errorf("meta stats = rows %d, bytes %d", s.MetaRows, s.MetaBytes)
	}
	if s.SchemaVersion != store.LatestSchemaVersion() || s.BinarySchemaVersion != store.LatestSchemaVersion() {
		t.Errorf("schema versions = %d/%d", s.SchemaVersion, s.BinarySchemaVersion)
	}
	if s.LastIngest["claude"] == "" {
		t.Errorf("status = %+v", s)
	}
	if s.RawRows != 9 || s.RawBytes <= 0 || s.RawTruncated != 0 || s.RawRatio <= 1 {
		t.Errorf("raw stats = rows %d, bytes %d, truncated %d, ratio %.2f", s.RawRows, s.RawBytes, s.RawTruncated, s.RawRatio)
	}
	if s.ErrorsWindow != "24h" || string(s.ErrorsSince) != isoTime(e.now.Add(-24*time.Hour)) || s.OlderErrors != 1 || s.RecentErrorsTotal != 1 {
		t.Errorf("error window = %+v", s)
	}
	if len(s.RecentErrors) != 1 || s.RecentErrors[0].Message != "hook Stop: boom" || string(s.RecentErrors[0].At) != isoTime(e.now.Add(-3*time.Hour)) {
		t.Errorf("recent errors = %+v", s.RecentErrors)
	}
	out := e.mustRun(t, "status")
	for _, want := range []string{e.db, "schema", "in 9 events, compressed", "meta records", "in 1 records", "last ingest (claude)", "none in", "recent errors (last 24h", "2026-10-20 11:00  (3h00m ago)  hook Stop: boom", "older errors: 1", "--all-errors"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "permission denied") || strings.Contains(out, "no such file") {
		t.Errorf("status shows old or skipped errors:\n%s", out)
	}
	// --all-errors lifts the window; missing transcripts stay hidden.
	s = decode[StatusJSON](t, e.mustRun(t, "status", "--json", "--all-errors"))
	if s.ErrorsWindow != "" || s.ErrorsSince != "" || s.OlderErrors != 0 || s.RecentErrorsTotal != 2 || len(s.RecentErrors) != 2 || s.RecentErrors[0].Message != "hook Stop: /p/a.jsonl: permission denied" {
		t.Errorf("--all-errors = %+v", s)
	}
	out = e.mustRun(t, "status", "--all-errors")
	if !strings.Contains(out, "errors (all): 2") || !strings.Contains(out, "permission denied") || strings.Contains(out, "older errors") {
		t.Errorf("--all-errors text:\n%s", out)
	}
	// --errors caps the listed lines, the totals stay.
	s = decode[StatusJSON](t, e.mustRun(t, "status", "--json", "--all-errors", "--errors", "1"))
	if s.RecentErrorsTotal != 2 || len(s.RecentErrors) != 1 || s.RecentErrors[0].Message != "hook Stop: boom" {
		t.Errorf("--errors 1 = %+v", s)
	}
	if out := e.mustRun(t, "status", "--all-errors", "--errors", "1"); !strings.Contains(out, "errors (all): 2, newest 1 shown") {
		t.Errorf("--errors 1 text:\n%s", out)
	}
	// Without errors in the window the text says so.
	if err := os.Remove(e.log); err != nil {
		t.Fatal(err)
	}
	logfile.New(e.log, at(-72*time.Hour)).Errorf("hook Stop: old")
	s = decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
	if len(s.RecentErrors) != 0 || s.RecentErrorsTotal != 0 || s.OlderErrors != 1 {
		t.Errorf("only old = %+v", s)
	}
	if out := e.mustRun(t, "status"); !strings.Contains(out, "recent errors (last 24h, since 2026-10-19 14:00): none") || !strings.Contains(out, "older errors: 1") {
		t.Errorf("only old text:\n%s", out)
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
	want := filepath.Join(e.backups, "archive-2026-10-20.db.zst")
	if !strings.Contains(out, want) {
		t.Errorf("backup = %q", out)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
	// The date uses the configured time zone: 23:30 UTC is the next day in Berlin.
	e.now = time.Date(2026, 10, 21, 23, 30, 0, 0, time.UTC)
	if out := e.mustRun(t, "backup"); !strings.Contains(out, "archive-2026-10-22.db.zst") {
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

// TestBackupSameDay: a second copy of a day stays next to the first in any
// format, --if-older and status see it, and retention logs what it deletes.
func TestBackupSameDay(t *testing.T) {
	tests := []struct {
		name  string
		first string
	}{
		{"same format", "archive-2026-10-20.db.zst"},
		{"gzip of the day", "archive-2026-10-20.db.gz"},
		{"plain of the day", "archive-2026-10-20.db"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			e.mustRun(t, "ingest", "--all")
			if err := os.MkdirAll(e.backups, 0o755); err != nil {
				t.Fatal(err)
			}
			first := filepath.Join(e.backups, tt.first)
			if err := os.WriteFile(first, []byte("the copy of the morning"), 0o644); err != nil {
				t.Fatal(err)
			}
			// 12:00 UTC is 14:00 in the configured time zone.
			timed := filepath.Join(e.backups, "archive-2026-10-20-1400.db.zst")
			if out := e.mustRun(t, "backup"); !strings.Contains(out, timed) {
				t.Fatalf("second copy of the day = %q", out)
			}
			if b, err := os.ReadFile(first); err != nil || string(b) != "the copy of the morning" {
				t.Errorf("first copy: %q %v", b, err)
			}
			e.now = time.Now()
			if out := e.mustRun(t, "backup", "--if-older", "24h"); !strings.Contains(out, "skipped: "+timed) {
				t.Errorf("--if-older with a timed copy = %q", out)
			}
			if out := e.mustRun(t, "status"); !strings.Contains(out, timed) {
				t.Errorf("status does not show the timed copy: %q", out)
			}
		})
	}
}

// TestBackupLogsPruned: the log file names each copy retention deleted.
func TestBackupLogsPruned(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	if err := os.MkdirAll(e.backups, 0o755); err != nil {
		t.Fatal(err)
	}
	var old []string
	for d := 1; d <= 9; d++ {
		p := filepath.Join(e.backups, fmt.Sprintf("archive-2026-10-%02d.db.gz", d))
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		old = append(old, p)
	}
	e.mustRun(t, "backup")
	log, err := os.ReadFile(e.log)
	if err != nil {
		t.Fatal(err)
	}
	// keep_daily 7 keeps the new day and the six days before it; the newest
	// copy of October is one of them.
	for _, p := range old[:3] {
		if !strings.Contains(string(log), "pruned "+p) {
			t.Errorf("log does not name %s:\n%s", p, log)
		}
		if _, err := os.Stat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s not pruned: %v", p, err)
		}
	}
	if strings.Contains(string(log), "pruned "+old[3]) {
		t.Errorf("log names a kept copy:\n%s", log)
	}
}

func TestRestore(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	e.mustRun(t, "backup")
	bak := filepath.Join(e.backups, "archive-2026-10-20.db.zst")
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

// TestAccountsAndContinuation: [accounts] names the account of session 1,
// a third session is continued in session 2, and sessions, show, search
// and stats expose both.
func TestAccountsAndContinuation(t *testing.T) {
	e := newTestEnv(t)
	tf.AppendFile(t, e.configPath, "\n[accounts]\n\""+tf.Account1+"\" = \"work\"\n")
	s3 := "33333333-3333-4333-8333-333333333333"
	tf.WriteFile(t, e.projects, "-w-beta/"+s3+".jsonl", tf.Join(
		tf.User(s3, "f1", "2026-10-06T13:00:00.000Z", "/w/beta", "draft the release notes"),
		tf.Assistant(s3, "f2", "2026-10-06T13:01:00.000Z", "/w/beta", "Draft written."),
		tf.ContinuedIn(s3, "2026-10-06T13:55:00.000Z", tf.SmallSession2),
	))
	e.mustRun(t, "ingest", "--all")

	byID := map[string]SessionJSON{}
	for _, s := range decode[[]SessionJSON](t, e.mustRun(t, "sessions", "--json")) {
		byID[s.NativeID] = s
	}
	s1, s2, s3j := byID[tf.SmallSession1], byID[tf.SmallSession2], byID[s3]
	if s1.Account != "work" || s1.AccountID != NullString(tf.Account1) {
		t.Errorf("session 1 = %+v", s1)
	}
	if s3j.ContinuedIn != NullString("claude:"+tf.SmallSession2) || s3j.ContinuedFrom != "" || s3j.EndedAt != "2026-10-06T13:01:00.000Z" {
		t.Errorf("session 3 = %+v", s3j)
	}
	if s2.ContinuedFrom != NullString("claude:"+s3) || s2.ContinuedIn != "" {
		t.Errorf("session 2 = %+v", s2)
	}
	for _, tt := range []struct {
		account string
		want    int
	}{{"work", 1}, {tf.Account1, 1}, {"acc0", 1}, {"nope", 0}} {
		if list := decode[[]SessionJSON](t, e.mustRun(t, "sessions", "--json", "--account", tt.account)); len(list) != tt.want {
			t.Errorf("sessions --account %s = %d sessions, want %d", tt.account, len(list), tt.want)
		}
	}
	if hits := decode[[]HitJSON](t, e.mustRun(t, "search", "files", "--json", "--account", "work")); len(hits) == 0 {
		t.Errorf("search --account work: no hits")
	}
	if hits := decode[[]HitJSON](t, e.mustRun(t, "search", "files", "--json", "--account", "nope")); len(hits) != 0 {
		t.Errorf("search --account nope = %+v", hits)
	}

	text := e.mustRun(t, "show", s3[:8])
	if !strings.Contains(text, "continued in") || !strings.Contains(text, "claude:"+tf.SmallSession2) {
		t.Errorf("show %s lacks the continuation:\n%s", s3[:8], text)
	}
	text = e.mustRun(t, "show", tf.SmallSession2[:8])
	if !strings.Contains(text, "continued from") || !strings.Contains(text, "claude:"+s3) {
		t.Errorf("show %s lacks the origin:\n%s", tf.SmallSession2[:8], text)
	}
	text = e.mustRun(t, "show", tf.SmallSession1[:8])
	if !strings.Contains(text, "account") || !strings.Contains(text, "work ("+tf.Account1+")") || !strings.Contains(text, tf.Organization1) {
		t.Errorf("show %s lacks the account:\n%s", tf.SmallSession1[:8], text)
	}

	series := decode[ActivitySeriesJSON](t, e.mustRun(t, "stats", "--json", "--month", "2026-10", "--period", "month", "--by", "account"))
	if series.By != "account" || len(series.Keys) != 2 || series.Keys[0] != "work" || series.Keys[1] != "unknown" {
		t.Errorf("stats --by account keys = %+v", series)
	}
	if tbl := e.mustRun(t, "stats", "--month", "2026-10", "--period", "month", "--by", "account"); !strings.Contains(tbl, "work") || !strings.Contains(tbl, "unknown") {
		t.Errorf("stats --by account table:\n%s", tbl)
	}
}

// TestIngestReread: --reread reads every source again from the start
// without duplicating events or full-text rows, and refuses to run
// without sources.
func TestIngestReread(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	if r := e.run(t, "", "ingest", "--reread"); r.code == 0 || !strings.Contains(r.stderr, "--reread") {
		t.Errorf("reread without sources = %+v", r)
	}
	counts := func() string {
		return e.mustRun(t, "sql", "--format", "csv", "SELECT (SELECT count(*) FROM events), (SELECT count(*) FROM events_fts), (SELECT count(*) FROM sessions)")
	}
	before := counts()
	for range 2 {
		out := e.mustRun(t, "ingest", "--all", "--reread")
		if !strings.Contains(out, "scanned 4 files: 4 changed") || !strings.Contains(out, "0 new events") {
			t.Errorf("reread summary = %q", out)
		}
		if got := counts(); got != before {
			t.Errorf("counts after reread = %q, want %q", got, before)
		}
	}
	p := filepath.Join(e.projects, "-w-beta", tf.SmallSession2+".jsonl")
	if out := e.mustRun(t, "ingest", "--reread", p); !strings.Contains(out, "scanned 1 files: 1 changed") || !strings.Contains(out, "0 new events") {
		t.Errorf("reread one file = %q", out)
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
		origins[v.Key] = v.Origin + "=" + string(v.Value)
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
	if out := e.mustRun(t, "completion", "zsh"); !strings.Contains(out, "total-recall") {
		t.Error("completion command missing")
	}
}

// TestVersion: the subcommand and the flag print the build version, nothing
// else, the same way.
func TestVersion(t *testing.T) {
	e := newTestEnv(t)
	want := version.Version + "\n"
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"subcommand", []string{"version"}},
		{"flag", []string{"--version"}},
		{"short flag", []string{"-v"}},
		{"flag after global flag", []string{"--config", e.configPath, "--version"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := e.run(t, "", tc.args...)
			if r.code != 0 || r.stdout != want || r.stderr != "" {
				t.Errorf("%v = %+v, want stdout %q", tc.args, r, want)
			}
		})
	}
	if r := e.run(t, "", "version", "extra"); r.code == 0 {
		t.Error("version with an argument succeeded")
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

func TestTUICommand(t *testing.T) {
	e := newTestEnv(t)
	// Flags and arguments are validated before anything is opened, so these
	// never start the program.
	if out := e.mustRun(t, "tui", "--help"); !strings.Contains(out, "--graphics") || !strings.Contains(out, "--query") || !strings.Contains(out, "--no-mouse") {
		t.Errorf("tui --help = %q", out)
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"bad graphics", []string{"tui", "--graphics", "bogus"}, "graphics"},
		{"bad view", []string{"tui", "bogus"}, "view"},
		{"too many args", []string{"tui", "search", "stats"}, "accepts"},
		{"no archive", []string{"tui", "stats", "--graphics", "text", "--query", "x"}, "no archive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := e.run(t, "", tt.args...)
			if r.code == 0 || !strings.Contains(r.stderr, tt.want) {
				t.Errorf("%v = %+v, want exit != 0 and %q in stderr", tt.args, r, tt.want)
			}
		})
	}
}

// TestTUIOptions checks the precedence flag > config > default for the
// [tui] table and that the key map reaches the app.
func TestTUIOptions(t *testing.T) {
	base := func() *config.Config {
		return &config.Config{Timezone: "UTC",
			Work: config.Work{Hours: "09:00-17:00", Target: config.Duration{Duration: 8 * time.Hour}, DailyHigh: config.Duration{Duration: 9 * time.Hour}, WeeklyHigh: config.Duration{Duration: 48 * time.Hour}, Count: "human"},
			TUI:  config.TUI{View: "search", Graphics: "auto", Mouse: true, StatsPeriod: "day", StatsDimension: "project", ActivityWindow: "week", ActivityNewest: "bottom", Reload: config.Duration{Duration: 5 * time.Second}}}
	}
	tests := []struct {
		name    string
		cfg     func(*config.Config)
		flags   tuiFlags
		want    tui.Options
		wantErr string
	}{
		{"defaults", nil, tuiFlags{},
			tui.Options{Start: "search", Graphics: "auto", NoMouse: false, StatsPeriod: "day", StatsDimension: "project", ActivityWindow: "week", ActivityNewest: "bottom", Reload: 5 * time.Second}, ""},
		{"config", func(c *config.Config) {
			c.TUI = config.TUI{View: "stats", Graphics: "text", Mouse: false, StatsPeriod: "week", StatsDimension: "agent", ActivityWindow: "month", ActivityNewest: "top", Reload: config.Duration{Duration: 10 * time.Second}}
		}, tuiFlags{},
			tui.Options{Start: "stats", Graphics: "text", NoMouse: true, StatsPeriod: "week", StatsDimension: "agent", ActivityWindow: "month", ActivityNewest: "top", Reload: 10 * time.Second}, ""},
		{"flags over config", func(c *config.Config) {
			c.TUI = config.TUI{View: "stats", Graphics: "text", Mouse: false, StatsPeriod: "week", StatsDimension: "agent", ActivityWindow: "month", ActivityNewest: "top", Reload: config.Duration{Duration: 10 * time.Second}}
		}, tuiFlags{View: "activity", Graphics: "kitty", GraphicsSet: true, NoMouse: false, NoMouseSet: true, Query: "x", Reload: 0, ReloadSet: true},
			tui.Options{Start: "activity", Graphics: "kitty", NoMouse: false, Query: "x", StatsPeriod: "week", StatsDimension: "agent", ActivityWindow: "month", ActivityNewest: "top", Reload: 0}, ""},
		{"unset bool flag keeps config", func(c *config.Config) { c.TUI.Mouse = false }, tuiFlags{NoMouse: false},
			tui.Options{Start: "search", Graphics: "auto", NoMouse: true, StatsPeriod: "day", StatsDimension: "project", ActivityWindow: "week", ActivityNewest: "bottom", Reload: 5 * time.Second}, ""},
		{"reload flag", nil, tuiFlags{Reload: 2 * time.Second, ReloadSet: true},
			tui.Options{Start: "search", Graphics: "auto", StatsPeriod: "day", StatsDimension: "project", ActivityWindow: "week", ActivityNewest: "bottom", Reload: 2 * time.Second}, ""},
		{"reload flag too short", nil, tuiFlags{Reload: 500 * time.Millisecond, ReloadSet: true}, tui.Options{}, "--reload 500ms"},
		{"bad reload in config", func(c *config.Config) { c.TUI.Reload = config.Duration{Duration: time.Millisecond} }, tuiFlags{}, tui.Options{}, "tui.reload"},
		{"bad view flag", nil, tuiFlags{View: "bogus"}, tui.Options{}, `unknown view "bogus"`},
		{"bad keys table", func(c *config.Config) { c.Keys = map[string]config.KeyList{"stats_days": {"e"}} }, tuiFlags{}, tui.Options{}, "stats_metric"},
		{"bad tui table", func(c *config.Config) { c.TUI.View = "help" }, tuiFlags{View: "search"}, tui.Options{}, "tui.view"},
		{"bad work table", func(c *config.Config) { c.Work.Hours = "9-17" }, tuiFlags{}, tui.Options{}, "work.hours"},
		{"bad project list", func(c *config.Config) { c.Projects = []config.Project{{Name: "x"}} }, tuiFlags{}, tui.Options{}, "project[0]"},
		{"bad graphics flag", nil, tuiFlags{Graphics: "sixel", GraphicsSet: true}, tui.Options{}, "graphics"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			if tt.cfg != nil {
				tt.cfg(cfg)
			}
			got, err := tuiOptions(cfg, tt.flags)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("tuiOptions error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Keys == nil || got.Holidays == nil || !got.HumanOnly {
				t.Errorf("keys, holidays or human-only not set: %+v", got)
			}
			got.Keys, got.Location, got.Work, got.Holidays, got.HumanOnly = nil, nil, store.WorkHours{}, nil, false
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("tuiOptions = %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestDeferredConfigErrors: a broken [keys] or [work] table must never stop
// archiving. Hooks and search run, tui and stats --workday fail with the
// error, config show and doctor report it.
func TestDeferredConfigErrors(t *testing.T) {
	e := newTestEnv(t)
	b, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configPath, append(b, []byte("\n[work]\nhours = \"9-17\"\n[keys]\nstats_days = \"e\"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := e.run(t, e.hookPayload(t, "Stop"), "ingest", "--hook"); r.code != 0 {
		t.Fatalf("hook with a broken [keys] table: %+v", r)
	}
	if n := e.count(t, "select count(*) from sessions"); n == 0 {
		t.Error("hook archived nothing")
	}
	if r := e.run(t, "", "search", "x"); r.code != 0 {
		t.Errorf("search with a broken [keys] table: %+v", r)
	}
	fails := [][]string{{"tui"}, {"stats", "--workday"}}
	for _, args := range fails {
		r := e.run(t, "", args...)
		if r.code == 0 || !strings.Contains(r.stderr, "config:") {
			t.Errorf("%v = %+v, want failure with the config error", args, r)
		}
	}
	r := e.run(t, "", "config", "show")
	if r.code == 0 || !strings.Contains(r.stdout, "ORIGIN") || !strings.Contains(r.stdout, "stats_metric") || !strings.Contains(r.stdout, "work.hours") {
		t.Errorf("config show = %+v, want the table plus both problems and exit != 0", r)
	}
	r = e.run(t, "", "config", "show", "--json")
	c := decode[ConfigJSON](t, r.stdout)
	if r.code == 0 || len(c.Problems) != 2 || !strings.Contains(c.Problems[1], "stats_metric") {
		t.Errorf("config show --json = exit %d, problems %v", r.code, c.Problems)
	}
	r = e.run(t, "", "doctor")
	if r.code != 2 || !strings.Contains(r.stdout, "stats_metric") || !strings.Contains(r.stdout, "work.hours") {
		t.Errorf("doctor = %+v, want exit 2 and the problems", r)
	}
	r = e.run(t, "", "doctor", "--json")
	if r.code != 2 || !strings.Contains(r.stderr, "stats_metric") {
		t.Errorf("doctor --json = %+v, want exit 2 and the problems on stderr", r)
	}
	if reps := decode[[]harness.FormatReport](t, r.stdout); len(reps) != 1 {
		t.Errorf("doctor --json reports = %+v", reps)
	}
}

// TestShowWindow covers --since/--until (local time per config), --limit,
// --offset and --from-event of show. Small session 1 has four conversation
// events at 08:00:00Z, 08:00:02Z, 09:30:00Z and 09:30:05Z, i.e. 10:00 and
// 11:30 in Europe/Berlin (CEST).
func TestShowWindow(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	type showOut struct {
		Session SessionJSON `json:"session"`
		Events  []EventJSON `json:"events"`
	}
	full := decode[showOut](t, e.mustRun(t, "show", tf.SmallSession1, "--json"))
	if len(full.Events) != 4 {
		t.Fatalf("show = %d events", len(full.Events))
	}
	third := full.Events[2].ID
	tests := []struct {
		name string
		args []string
		want []string // event texts
	}{
		{"since at first event (local)", []string{"--since", "2026-10-05T10:00"}, []string{"please list the files", "There are two files.", "thanks, now count the lines", "Forty-two lines."}},
		{"since after first pair", []string{"--since", "2026-10-05T10:01"}, []string{"thanks, now count the lines", "Forty-two lines."}},
		{"since with space", []string{"--since", "2026-10-05 11:30"}, []string{"thanks, now count the lines", "Forty-two lines."}},
		{"since RFC 3339 UTC", []string{"--since", "2026-10-05T09:30:00Z"}, []string{"thanks, now count the lines", "Forty-two lines."}},
		{"until exclusive", []string{"--until", "2026-10-05T11:30"}, []string{"please list the files", "There are two files."}},
		{"until date is whole day", []string{"--until", "2026-10-05"}, []string{"please list the files", "There are two files.", "thanks, now count the lines", "Forty-two lines."}},
		{"until before session", []string{"--until", "2026-10-04"}, []string{}},
		{"since and until", []string{"--since", "2026-10-05T10:00:01", "--until", "2026-10-05T11:30:01"}, []string{"There are two files.", "thanks, now count the lines"}},
		{"limit", []string{"--limit", "1"}, []string{"please list the files"}},
		{"limit and offset", []string{"--limit", "2", "--offset", "1"}, []string{"There are two files.", "thanks, now count the lines"}},
		{"offset only", []string{"--offset", "3"}, []string{"Forty-two lines."}},
		{"offset past end", []string{"--offset", "9"}, []string{}},
		{"from event", []string{"--from-event", third}, []string{"thanks, now count the lines", "Forty-two lines."}},
		{"from event with limit", []string{"--from-event", third, "--limit", "1"}, []string{"thanks, now count the lines"}},
		{"from event and until", []string{"--from-event", third, "--until", "2026-10-05T11:30:05"}, []string{"thanks, now count the lines"}},
		{"since with all", []string{"--all", "--since", "2026-10-05T10:00:01", "--until", "2026-10-05T10:01"}, []string{"ls", "There are two files."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"show", tf.SmallSession1[:8], "--json"}, tt.args...)
			sh := decode[showOut](t, e.mustRun(t, args...))
			if sh.Session.ID != "claude:"+tf.SmallSession1 {
				t.Errorf("session = %s", sh.Session.ID)
			}
			got := make([]string, 0, len(sh.Events))
			for _, ev := range sh.Events {
				got = append(got, string(ev.Text))
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("events = %q, want %q", got, tt.want)
			}
			// The text output lists the same events.
			text := e.mustRun(t, append([]string{"show", tf.SmallSession1[:8]}, tt.args...)...)
			if n := strings.Count(text, "\n── "); n != len(tt.want) {
				t.Errorf("text output has %d events, want %d:\n%s", n, len(tt.want), text)
			}
		})
	}
	// An empty window is an empty list, not null.
	if out := e.mustRun(t, "show", tf.SmallSession1[:8], "--json", "--until", "2026-10-04"); !strings.Contains(out, `"events": []`) {
		t.Errorf("empty window = %s", out)
	}
	bad := []struct {
		name string
		args []string
		msg  string
	}{
		{"bad since", []string{"--since", "yesterday"}, "invalid time"},
		{"bad until", []string{"--until", "2026-13-01"}, "invalid time"},
		{"negative offset", []string{"--offset", "-1"}, "--offset"},
		{"negative limit", []string{"--limit", "-1"}, "--limit"},
		{"unknown event", []string{"--from-event", "claude:nope"}, "no event"},
		{"from-event with bad since", []string{"--from-event", third, "--since", "x"}, "invalid time"},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			r := e.run(t, "", append([]string{"show", tf.SmallSession1[:8]}, tt.args...)...)
			if r.code == 0 || !strings.Contains(r.stderr, tt.msg) {
				t.Errorf("%v = exit %d, stderr %q, want %q", tt.args, r.code, r.stderr, tt.msg)
			}
		})
	}
	if r := e.run(t, "", "show", tf.SmallSession2[:8], "--from-event", third); r.code == 0 || !strings.Contains(r.stderr, "no event") {
		t.Errorf("from-event of another session = %+v", r)
	}
}

// TestShowHint checks the stderr hint for an unbounded show of a large
// session, and that no hint appears when the output is bounded.
func TestShowHint(t *testing.T) {
	e := newTestEnv(t)
	const big = "33333333-3333-4333-8333-333333333333"
	var lines []tf.Line
	for i := 0; i <= showHintEvents; i++ {
		lines = append(lines, tf.User(big, fmt.Sprintf("u%d", i), time.Date(2026, 10, 7, 8, 0, i, 0, time.UTC).Format("2006-01-02T15:04:05.000Z"), "/w/big", fmt.Sprintf("message %d", i)))
	}
	tf.WriteFile(t, e.projects, "-w-big/"+big+".jsonl", tf.Join(lines...))
	e.mustRun(t, "ingest", "--all")
	tests := []struct {
		name string
		args []string
		hint bool
	}{
		{"unbounded", nil, true},
		{"unbounded json", []string{"--json"}, true},
		{"limit", []string{"--limit", "10"}, false},
		{"since", []string{"--since", "2026-10-07"}, false},
		{"until", []string{"--until", "2026-10-07T10:00"}, false},
		{"from event", []string{"--from-event", "claude:u0"}, false},
		{"small session", []string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := big
			if tt.name == "small session" {
				id = tf.SmallSession1
			}
			r := e.run(t, "", append([]string{"show", id}, tt.args...)...)
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			if got := strings.Contains(r.stderr, "--limit"); got != tt.hint {
				t.Errorf("hint = %v, want %v (stderr %q)", got, tt.hint, r.stderr)
			}
			if tt.hint && !strings.Contains(r.stderr, strconv.Itoa(showHintEvents+1)) {
				t.Errorf("hint lacks the event count: %q", r.stderr)
			}
			if tt.name == "unbounded" && strings.Count(r.stdout, "\n── ") != showHintEvents+1 {
				t.Errorf("unbounded show is not complete: %d events", strings.Count(r.stdout, "\n── "))
			}
		})
	}
}

// TestSearchSession covers search --session with full IDs and prefixes.
func TestSearchSession(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	tests := []struct {
		name, session string
		query         []string
		want          int
		wantSession   string
	}{
		{"full id", "claude:" + tf.SmallSession2, []string{"release"}, 2, "claude:" + tf.SmallSession2},
		{"native id", tf.SmallSession2, []string{"release"}, 2, "claude:" + tf.SmallSession2},
		{"prefix", tf.SmallSession2[:4], []string{"release"}, 2, "claude:" + tf.SmallSession2},
		{"prefix of other session", tf.SmallSession1[:4], []string{"release"}, 0, ""},
		{"prefix with filters", tf.SmallSession1[:4], []string{"files", "--actor", "human"}, 1, "claude:" + tf.SmallSession1},
		{"subagent", "agent-a01", []string{"counted"}, 1, "claude:agent-a01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"search", "--json", "--session", tt.session}, tt.query...)
			hits := decode[[]HitJSON](t, e.mustRun(t, args...))
			if len(hits) != tt.want {
				t.Fatalf("hits = %+v, want %d", hits, tt.want)
			}
			for _, h := range hits {
				if h.SessionID != tt.wantSession {
					t.Errorf("hit in %s, want %s", h.SessionID, tt.wantSession)
				}
			}
		})
	}
	if r := e.run(t, "", "search", "release", "--session", "claude:"); r.code == 0 || !strings.Contains(r.stderr, "more than one session") {
		t.Errorf("ambiguous prefix = %+v", r)
	}
	if r := e.run(t, "", "search", "release", "--session", "zzz"); r.code == 0 || !strings.Contains(r.stderr, "no session") {
		t.Errorf("unknown session = %+v", r)
	}
	if out := e.mustRun(t, "search", "release", "--session", tf.SmallSession2[:8]); !strings.Contains(out, "Beta work") {
		t.Errorf("table = %q", out)
	}
}

// TestIngestRewriteRaw: --reread --rewrite-raw replaces raw of existing
// events, keeps every count and refuses to run without --reread (#130).
func TestIngestRewriteRaw(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	if r := e.run(t, "", "ingest", "--all", "--rewrite-raw"); r.code == 0 || !strings.Contains(r.stderr, "--reread") {
		t.Errorf("rewrite-raw without reread = %+v", r)
	}
	db, err := sql.Open("sqlite", e.db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE events SET raw = '{"text":"«redacted:high_entropy»"}'`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	counts := func() string {
		return e.mustRun(t, "sql", "--format", "csv", "SELECT (SELECT count(*) FROM events), (SELECT count(*) FROM events_fts), (SELECT count(*) FROM sessions)")
	}
	before := counts()
	out := e.mustRun(t, "ingest", "--all", "--reread", "--rewrite-raw")
	// 9 events and 1 meta record (the workflow journal) are rewritten.
	if !strings.Contains(out, "0 new events, 0 meta records, 9 events and 1 meta records raw rewritten") {
		t.Errorf("rewrite summary = %q", out)
	}
	if got := counts(); got != before {
		t.Errorf("counts after rewrite = %q, want %q", got, before)
	}
	if n := e.mustRun(t, "sql", "--format", "csv", "SELECT count(*) FROM events WHERE raw_text(raw) LIKE '%high_entropy%' OR raw IS NULL"); !strings.HasSuffix(strings.TrimSpace(n), "0") {
		t.Errorf("raw still masked: %q", n)
	}
}

// imageTranscript writes a session whose user pastes a small synthetic
// picture twice and returns its event ID prefix.
func imageTranscript(t *testing.T, e *testEnv, img []byte) string {
	t.Helper()
	const sid = "33333333-3333-4333-8333-333333333333"
	line := func(uuid, ts string) tf.Line {
		l := tf.User(sid, uuid, ts, "/w/pics", "")
		l["message"] = map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "look"},
			map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": base64.StdEncoding.EncodeToString(img)}},
		}}
		return l
	}
	tf.WriteFile(t, e.projects, "-w-pics/"+sid+".jsonl", line("p1", "2026-10-03T10:00:00Z").JSON()+line("p2", "2026-10-03T10:01:00Z").JSON())
	return "claude:" + sid
}

func TestBlobCommand(t *testing.T) {
	img := []byte("\x89PNG\r\n\x1a\nsynthetic picture for the blob command")
	e := newTestEnv(t)
	sid := imageTranscript(t, e, img)
	e.mustRun(t, "ingest", "--all")
	hash := blobref.Hash(img)

	// To a pipe (the test's stdout is not a terminal): the bytes.
	if out := e.mustRun(t, "blob", hash); out != string(img) {
		t.Errorf("blob to stdout = %q", out)
	}
	// A unique prefix works; to a file with a summary line.
	file := filepath.Join(e.dir, "pic.png")
	if out := e.mustRun(t, "blob", hash[:10], "--out", file); !strings.Contains(out, "wrote "+file+" (image/png, 46 B)") {
		t.Errorf("blob --out = %q", out)
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != string(img) {
		t.Errorf("written file = %q, %v", got, err)
	}
	// JSON: metadata and the events, never the data.
	b := decode[BlobJSON](t, e.mustRun(t, "blob", hash, "--json"))
	if b.SHA256 != hash || b.MediaType != "image/png" || b.Bytes != int64(len(img)) || b.StoredBytes != int64(len(img)) || b.Out != "" ||
		strings.Join(b.Events, ",") != "claude:p1#1,claude:p2#1" {
		t.Errorf("blob --json = %+v", b)
	}
	// On a terminal binary data is refused.
	e.width = 80
	if r := e.run(t, "", "blob", hash); r.code == 0 || !strings.Contains(r.stderr, "--out") {
		t.Errorf("blob on a terminal = %+v", r)
	}
	e.width = 0
	// Unknown and ambiguous references.
	if r := e.run(t, "", "blob", "0000", "--json"); r.code != 1 || !strings.Contains(r.stdout, `"kind":"not_found"`) {
		t.Errorf("unknown = %+v", r)
	}
	if r := e.run(t, "", "blob", "", "--json"); r.code != 1 || !strings.Contains(r.stdout, `"kind":"ambiguous"`) {
		t.Errorf("empty = %+v", r)
	}
	if out := e.mustRun(t, "show", sid, "--json"); !strings.Contains(out, hash) {
		t.Errorf("show names the hash: %q", out)
	}
}

func TestShowAttachments(t *testing.T) {
	img := []byte("\x89PNG\r\n\x1a\nsynthetic picture for show")
	e := newTestEnv(t)
	sid := imageTranscript(t, e, img)
	if out := e.mustRun(t, "ingest", "--all"); !strings.Contains(out, "1 attachments (0.0 MiB)") {
		t.Errorf("ingest summary = %q", out)
	}
	hash := blobref.Hash(img)
	out := e.mustRun(t, "show", sid)
	if !strings.Contains(out, "[image image/png 34 B]") || strings.Count(out, "   attachment "+hash+"  image/png  34 B") != 2 {
		t.Errorf("show = %q", out)
	}
	sh := decode[ShowJSON](t, e.mustRun(t, "show", sid, "--json"))
	if len(sh.Events) != 4 || len(sh.Events[0].Attachments) != 0 || len(sh.Events[1].Attachments) != 1 ||
		sh.Events[1].Attachments[0] != (AttachmentJSON{SHA256: hash, MediaType: "image/png", Bytes: 34, Position: 0}) {
		t.Errorf("show --json = %+v", sh.Events)
	}
	if s := decode[StatusJSON](t, e.mustRun(t, "status", "--json")); s.Blobs != 1 || s.BlobBytes != 34 || s.BlobStoredBytes != 34 {
		t.Errorf("status blobs = %d, %d, %d", s.Blobs, s.BlobBytes, s.BlobStoredBytes)
	}
	if out := e.mustRun(t, "status"); !strings.Contains(out, "1 attachments, 34 B (34 B stored)") {
		t.Errorf("status = %q", out)
	}
}

// TestAddSummaryCoversEveryField: a counter added to ingest.Summary and
// forgotten in addSummary silently prints 0 (#201).
func TestAddSummaryCoversEveryField(t *testing.T) {
	var one ingest.Summary
	v := reflect.ValueOf(&one).Elem()
	for i := range v.NumField() {
		switch f := v.Field(i); f.Kind() {
		case reflect.Int, reflect.Int64:
			f.SetInt(1)
		case reflect.Slice:
			f.Set(reflect.MakeSlice(f.Type(), 1, 1))
		default:
			t.Fatalf("field %s: kind %s not covered", v.Type().Field(i).Name, f.Kind())
		}
	}
	sum := reflect.ValueOf(addSummary(addSummary(ingest.Summary{}, one), one))
	for i := range sum.NumField() {
		f, name := sum.Field(i), v.Type().Field(i).Name
		if (f.Kind() == reflect.Slice && f.Len() != 2) || (f.Kind() != reflect.Slice && f.Int() != 2) {
			t.Errorf("addSummary drops %s", name)
		}
	}
}
