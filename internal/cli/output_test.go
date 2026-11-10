package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tf "github.com/seletz/total-recall/internal/testfixture"
)

// longWord is a cell value wider than any terminal in these tests.
var longWord = strings.Repeat("x", 300)

// TestJSONErrors checks the error contract of the read commands: with
// --json or --format json|jsonl an error is one JSON object on stdout,
// the message is on stderr as before and the exit code is 1.
func TestJSONErrors(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	broken := filepath.Join(e.dir, "broken.toml")
	if err := os.WriteFile(broken, []byte("db = [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		kind string
		msg  string
	}{
		{"sql json", []string{"sql", "--json", "select nosuchcol from sessions"}, "sql", "no such column"},
		{"sql jsonl", []string{"sql", "--format", "jsonl", "select nosuchcol from sessions"}, "sql", "no such column"},
		{"sessions time", []string{"sessions", "--json", "--since", "yesterday"}, "usage", "invalid time"},
		{"sessions unknown flag", []string{"sessions", "--json", "--bogus"}, "usage", "unknown flag"},
		{"sessions bad int", []string{"sessions", "--limit", "x", "--format=json"}, "usage", "invalid argument"},
		{"show not found", []string{"show", "zzzz", "--json"}, "not_found", "no session matches"},
		{"show ambiguous", []string{"show", "claude:", "--json"}, "ambiguous", "more than one session"},
		{"show args", []string{"show", "--json"}, "usage", "accepts 1 arg"},
		{"show since", []string{"show", tf.SmallSession1[:8], "--json", "--since", "soon"}, "usage", "invalid time"},
		{"show limit", []string{"show", tf.SmallSession1[:8], "--format", "jsonl", "--limit", "-1"}, "usage", "--limit must not be negative"},
		{"show offset", []string{"show", tf.SmallSession1[:8], "--json", "--offset", "-1"}, "usage", "--offset must not be negative"},
		{"show from-event", []string{"show", tf.SmallSession1[:8], "--json", "--from-event", "nope"}, "not_found", "no event"},
		{"search session not found", []string{"search", "release", "--json", "--session", "zzzz"}, "not_found", "no session matches"},
		{"search session ambiguous", []string{"search", "release", "--json", "--session", "claude:"}, "ambiguous", "more than one session"},
		{"search fts", []string{"search", "--raw", "--json", `"unbalanced`}, "sql", "unterminated string"},
		{"search by-session fts", []string{"search", "--by-session", "--raw", "--json", `"unbalanced`}, "sql", "unterminated string"},
		{"search by-session order", []string{"search", "--by-session", "--json", "--order", "rank", "x"}, "usage", "--order"},
		{"stats flags", []string{"stats", "--json", "--range", "today"}, "usage", "--range requires --workday"},
		{"stats month", []string{"stats", "--format", "jsonl", "--month", "10"}, "usage", "invalid --month"},
		{"status db", []string{"--db", filepath.Join(e.dir, "none.db"), "status", "--json"}, "db", "no archive"},
		{"config", []string{"--config", broken, "sessions", "--json"}, "config", ""},
		{"doctor harness", []string{"doctor", "--json", "--harness", "nope"}, "usage", "no enabled harness"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := e.run(t, "", tt.args...)
			if r.code != 1 {
				t.Fatalf("exit %d, want 1\nstdout: %s\nstderr: %s", r.code, r.stdout, r.stderr)
			}
			if n := strings.Count(strings.TrimSpace(r.stdout), "\n"); n != 0 {
				t.Errorf("stdout has %d lines, want 1:\n%s", n+1, r.stdout)
			}
			ej := decode[ErrorJSON](t, r.stdout)
			if ej.Error.Kind != tt.kind || !strings.Contains(ej.Error.Message, tt.msg) {
				t.Errorf("error = %+v, want kind %q with %q", ej.Error, tt.kind, tt.msg)
			}
			if !strings.Contains(r.stderr, ej.Error.Message) {
				t.Errorf("stderr %q lacks the message %q", r.stderr, ej.Error.Message)
			}
		})
	}
	t.Run("no json", func(t *testing.T) {
		r := e.run(t, "", "sql", "select nosuchcol from sessions")
		if r.code != 1 || r.stdout != "" || !strings.Contains(r.stderr, "no such column") {
			t.Errorf("plain error = %+v", r)
		}
	})
	t.Run("invalid format", func(t *testing.T) {
		r := e.run(t, "", "sessions", "--format", "xml")
		if r.code != 1 || r.stdout != "" || !strings.Contains(r.stderr, `invalid --format "xml" (table, json, jsonl)`) {
			t.Errorf("invalid format = %+v", r)
		}
		r = e.run(t, "", "sessions", "--format", "csv")
		if r.code != 1 || !strings.Contains(r.stderr, "invalid --format") {
			t.Errorf("csv on sessions = %+v", r)
		}
	})
	t.Run("empty is a list", func(t *testing.T) {
		if out := strings.TrimSpace(e.mustRun(t, "sessions", "--json", "--since", "2030-01-01")); out != "[]" {
			t.Errorf("empty = %q", out)
		}
	})
}

// TestJSONKeys checks that every documented key is present in the JSON
// output of the read commands and that empty values are null.
func TestJSONKeys(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	first := func(v any) map[string]any { return v.([]any)[0].(map[string]any) }
	obj := func(v any) map[string]any { return v.(map[string]any) }
	subagent := func(v any) map[string]any {
		for _, x := range v.([]any) {
			if m := x.(map[string]any); strings.HasPrefix(m["native_id"].(string), "agent-") {
				return m
			}
		}
		t.Fatal("no subagent session")
		return nil
	}
	tests := []struct {
		name  string
		args  []string
		pick  func(any) map[string]any
		keys  []string
		nulls []string
	}{
		{"sessions", []string{"sessions", "--json"}, first,
			[]string{"id", "harness", "native_id", "parent_id", "display_name", "name", "ai_title", "agent_name", "cwd",
				"git_root", "git_remote_url", "repo", "git_branch", "model", "harness_version", "started_at", "ended_at",
				"duration_seconds", "human_events", "agent_events", "events"},
			[]string{"parent_id", "agent_name"}},
		{"sessions subagent", []string{"sessions", "--json", "--subagents"}, subagent,
			[]string{"parent_id", "name", "ai_title", "agent_name"}, []string{"name", "ai_title"}},
		{"show", []string{"show", tf.SmallSession1[:8], "--json"},
			func(v any) map[string]any { return first(obj(v)["events"]) },
			[]string{"id", "ts", "kind", "role", "actor", "text", "tool_name", "model", "tokens_in", "tokens_out"},
			[]string{"tool_name", "model"}},
		{"show session", []string{"show", tf.SmallSession1[:8], "--json"},
			func(v any) map[string]any { return obj(obj(v)["session"]) },
			[]string{"id", "parent_id", "name"}, []string{"parent_id", "name"}},
		{"search", []string{"search", "release", "--json"}, first,
			[]string{"session_id", "session_name", "repo", "event_id", "ts", "actor", "kind", "snippet", "rank"}, nil},
		{"search by-session", []string{"search", "release", "--by-session", "--json"}, first,
			[]string{"session_id", "display_name", "repo", "first_hit", "last_hit", "hits", "snippet"}, nil},
		{"status", []string{"status", "--json"}, obj,
			[]string{"db", "db_size_bytes", "schema_version", "binary_schema_version", "sessions", "events", "sources",
				"last_ingest", "backup_dir", "last_backup", "last_backup_at", "log", "recent_errors"},
			[]string{"last_backup", "last_backup_at"}},
		{"stats workday", []string{"stats", "--workday", "--range", "this-week", "--json"},
			func(v any) map[string]any { return first(obj(v)["days"]) },
			[]string{"day", "weekday", "first", "last", "first_local", "last_local", "span_seconds", "weekend", "holiday",
				"target_seconds", "diff_seconds", "sessions", "human", "agent"},
			[]string{"first", "last", "first_local", "last_local", "holiday"}},
		{"stats series", []string{"stats", "--period", "week", "--since", "2026-10-01", "--until", "2026-10-31", "--json"}, obj,
			[]string{"since", "until", "timezone", "period", "by", "keys", "total", "by_key", "buckets"}, []string{"by"}},
		{"config show", []string{"config", "show", "--json"}, obj,
			[]string{"file", "file_origin", "loaded", "values", "problems"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.pick(decode[any](t, e.mustRun(t, tt.args...)))
			for _, k := range tt.keys {
				if _, ok := m[k]; !ok {
					t.Errorf("key %q missing in %v", k, m)
				}
			}
			for _, k := range tt.nulls {
				if v, ok := m[k]; !ok || v != nil {
					t.Errorf("key %q = %#v, want null", k, v)
				}
			}
		})
	}
	t.Run("config problems is a list", func(t *testing.T) {
		c := decode[map[string]any](t, e.mustRun(t, "config", "show", "--json"))
		if p, ok := c["problems"].([]any); !ok || len(p) != 0 {
			t.Errorf("problems = %#v, want []", c["problems"])
		}
	})
}

// TestTableTruncation checks the truncation rule of --format table: cells
// are cut to the terminal width only when stdout is a terminal, never when
// piped, never with --no-truncate; a cut cell ends with "…" and a hint with
// the count goes to stderr.
func TestTableTruncation(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	tf.WriteFile(t, e.projects, "-w-beta/44444444-4444-4444-8444-444444444444.jsonl", tf.Join(
		tf.CustomTitle("44444444-4444-4444-8444-444444444444", "A session with a title "+longWord),
		tf.User("44444444-4444-4444-8444-444444444444", "d1", "2026-10-07T10:00:00.000Z", "/w/beta", "verylongword "+longWord+" end"),
	))
	e.mustRun(t, "ingest", "--all")
	q := "select '" + longWord + "' as long_value, 1 as n"
	tests := []struct {
		name  string
		width int // terminal width; 0 = stdout is not a terminal
		args  []string
		cut   int // truncated cells expected
	}{
		{"sql piped", 0, []string{"sql", q}, 0},
		{"sql tty", 60, []string{"sql", q}, 1},
		{"sql tty no-truncate", 60, []string{"sql", "--no-truncate", q}, 0},
		{"sql tty wide", 400, []string{"sql", q}, 0},
		{"sql csv tty", 60, []string{"sql", "--format", "csv", q}, 0},
		{"sessions piped", 0, []string{"sessions"}, 0},
		{"sessions tty", 80, []string{"sessions"}, 1},
		{"sessions tty no-truncate", 80, []string{"sessions", "--no-truncate"}, 0},
		{"search piped", 0, []string{"search", "verylongword"}, 0},
		{"search tty", 80, []string{"search", "verylongword"}, 2}, // the long title and the snippet
		{"search tty no-truncate", 80, []string{"search", "verylongword", "--no-truncate"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.width = tt.width
			r := e.run(t, "", tt.args...)
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			full := strings.Contains(r.stdout, longWord)
			if full != (tt.cut == 0) {
				t.Errorf("full value present = %v, want %v:\n%s", full, tt.cut == 0, r.stdout)
			}
			if got := strings.Count(r.stdout, "…"); got != tt.cut {
				t.Errorf("%d cells end with …, want %d:\n%s", got, tt.cut, r.stdout)
			}
			hint := strings.Contains(r.stderr, "truncated")
			if hint != (tt.cut > 0) {
				t.Errorf("hint on stderr = %v, want %v: %q", hint, tt.cut > 0, r.stderr)
			}
			if tt.cut > 0 && !strings.Contains(r.stderr, "--no-truncate") {
				t.Errorf("hint does not name --no-truncate: %q", r.stderr)
			}
			if tt.width > 0 && tt.cut > 0 && tt.args[0] != "search" { // search lines have a fixed part
				for _, line := range strings.Split(strings.TrimRight(r.stdout, "\n"), "\n") {
					if n := len([]rune(line)); n > tt.width {
						t.Errorf("line of %d runes exceeds width %d: %q", n, tt.width, line)
					}
				}
			}
		})
	}
	t.Run("table keeps one line per row", func(t *testing.T) {
		e.width = 0
		out := e.mustRun(t, "sql", "select 'a\nb' as v")
		if lines := strings.Split(strings.TrimRight(out, "\n"), "\n"); len(lines) != 2 || !strings.Contains(lines[1], "a b") {
			t.Errorf("multi-line cell = %q", out)
		}
	})
}

// TestJSONL checks that --format jsonl writes one compact object per line.
func TestJSONL(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	tests := []struct {
		name  string
		args  []string
		lines int
	}{
		{"sql", []string{"sql", "--format", "jsonl", "select id from sessions order by id"}, 4},
		{"sessions", []string{"sessions", "--format", "jsonl"}, 2},
		{"search", []string{"search", "release", "--format", "jsonl"}, 2},
		{"search by-session", []string{"search", "release", "--format", "jsonl", "--by-session"}, 1},
		{"show", []string{"show", tf.SmallSession1[:8], "--format", "jsonl"}, 4},
		{"status", []string{"status", "--format", "jsonl"}, 1},
		{"stats", []string{"stats", "--month", "2026-10", "--format", "jsonl"}, 1},
		{"empty", []string{"sessions", "--format", "jsonl", "--since", "2030-01-01"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := e.mustRun(t, tt.args...)
			if out == "" {
				if tt.lines != 0 {
					t.Fatalf("no output, want %d lines", tt.lines)
				}
				return
			}
			lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
			if len(lines) != tt.lines {
				t.Fatalf("%d lines, want %d:\n%s", len(lines), tt.lines, out)
			}
			for _, l := range lines {
				var m map[string]any
				if err := json.Unmarshal([]byte(l), &m); err != nil {
					t.Errorf("line %q: %v", l, err)
				}
			}
		})
	}
}

func TestFitColumns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		widths []int
		limit  int
		want   []int
	}{
		{"fits", []int{10, 20}, 80, []int{10, 20}},
		{"shrink widest", []int{10, 300}, 40, []int{10, 28}},
		{"shrink two", []int{100, 100, 5}, 59, []int{25, 25, 5}},
		{"minimum", []int{3, 300}, 8, []int{3, 4}},
		{"no limit", []int{300}, 0, []int{300}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fitColumns(append([]int(nil), tt.widths...), tt.limit)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fitColumns(%v, %d) = %v, want %v", tt.widths, tt.limit, got, tt.want)
			}
		})
	}
}

func TestJSONRequested(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"sessions", "--json"}, true},
		{[]string{"sessions", "--json=true"}, true},
		{[]string{"sql", "--format", "json", "select 1"}, true},
		{[]string{"sql", "--format=jsonl", "select 1"}, true},
		{[]string{"sql", "--format", "csv", "select 1"}, false},
		{[]string{"sessions"}, false},
		{[]string{"sql", "select '--json'"}, false},
		{[]string{"sql", "--", "--json"}, false},
	}
	for _, tt := range tests {
		if got := jsonRequested(tt.args); got != tt.want {
			t.Errorf("jsonRequested(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestNullString(t *testing.T) {
	t.Parallel()
	type v struct {
		A NullString `json:"a"`
		B NullString `json:"b"`
	}
	b, err := json.Marshal(v{A: "x"})
	if err != nil || string(b) != `{"a":"x","b":null}` {
		t.Errorf("marshal = %s, %v", b, err)
	}
	var back v
	if err := json.Unmarshal([]byte(`{"a":null,"b":"y"}`), &back); err != nil || back.A != "" || back.B != "y" {
		t.Errorf("unmarshal = %+v, %v", back, err)
	}
}
