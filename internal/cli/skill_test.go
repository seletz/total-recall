package cli

import (
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/skill"
	"github.com/seletz/total-recall/internal/store"
	tf "github.com/seletz/total-recall/internal/testfixture"
)

// The skill (internal/skill/SKILL.md) documents the JSON fields of the read
// commands and the columns of the schema. These tests keep the document
// honest: every documented name must exist in the real output and the real
// schema, and vice versa. They also cover the output contract (#81: JSON
// errors on stdout, every key always present, jsonl, --no-truncate) and the
// show window and search --session flags (#82) as the skill describes them.

// skillSections parses SKILL.md into "heading -> documented names". A section
// is a heading whose text starts with a backticked name, optionally followed
// by a JSON path ("#### `show --json` events[]"); its names are the
// backticked first cells of the markdown table rows below it. Any other
// heading ends the section.
func skillSections(t *testing.T) map[string][]string {
	t.Helper()
	heading := regexp.MustCompile("^#+ `([^`]+)`(?: (\\S+))?\\s*$")
	row := regexp.MustCompile("^\\| `([^`]+)` \\|")
	out := map[string][]string{}
	key := ""
	for _, line := range strings.Split(skill.Text(), "\n") {
		if strings.HasPrefix(line, "#") {
			key = ""
			if m := heading.FindStringSubmatch(line); m != nil {
				key = strings.TrimSpace(m[1] + " " + m[2])
				out[key] = nil
			}
			continue
		}
		if key == "" {
			continue
		}
		if m := row.FindStringSubmatch(line); m != nil {
			out[key] = append(out[key], m[1])
		}
	}
	if len(out) == 0 {
		t.Fatal("SKILL.md: no documented sections found")
	}
	return out
}

// skillBlocks returns the fenced code blocks of SKILL.md with their language.
func skillBlocks() []struct{ lang, body string } {
	var out []struct{ lang, body string }
	lines := strings.Split(skill.Text(), "\n")
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "```") {
			continue
		}
		lang := strings.TrimPrefix(lines[i], "```")
		var body []string
		for i++; i < len(lines) && lines[i] != "```"; i++ {
			body = append(body, lines[i])
		}
		out = append(out, struct{ lang, body string }{lang, strings.Join(body, "\n")})
	}
	return out
}

// skillSession3 is an extra session only the skill tests ingest: a tool call
// with a result and token usage plus a named subagent, so every JSON key of
// sessions and show occurs in the fixture.
const skillSession3 = "33333333-3333-4333-8333-333333333333"

// skillEnv is the Demo fixture plus skillSession3, ingested.
func skillEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newTestEnv(t)
	s3 := skillSession3
	use := tf.ToolUse(s3, "d2", "2026-10-07T09:00:01.000Z", "/w/gamma", "Bash", "ls")
	use["message"].(map[string]any)["usage"] = map[string]any{"input_tokens": 10, "output_tokens": 5}
	tf.WriteFile(t, e.projects, "-w-gamma/"+s3+".jsonl", tf.Join(
		tf.User(s3, "d1", "2026-10-07T09:00:00.000Z", "/w/gamma", "we need an sbom for the release"),
		use,
		tf.ToolResult(s3, "d3", "2026-10-07T09:00:02.000Z", "/w/gamma", "d2", "a.go\nb.go"),
		tf.Assistant(s3, "d4", "2026-10-07T09:00:03.000Z", "/w/gamma", "Decided: syft generates the sbom in the release workflow."),
	))
	tf.WriteFile(t, e.projects, "-w-gamma/"+s3+"/subagents/agent-d01.jsonl", tf.Join(
		tf.Line{"type": "user", "uuid": "e1", "timestamp": "2026-10-07T09:00:10.000Z", "sessionId": s3, "agentId": "d01",
			"cwd": "/w/gamma", "message": map[string]any{"role": "user", "content": "Research task: sbom tools."}},
		tf.Line{"type": "assistant", "uuid": "e2", "timestamp": "2026-10-07T09:00:20.000Z", "sessionId": s3, "agentId": "d01",
			"cwd": "/w/gamma", "message": map[string]any{"id": "msg_e2", "model": "test-model", "role": "assistant",
				"content": []any{map[string]any{"type": "text", "text": "syft and cyclonedx."}}}},
		tf.Line{"type": "agent-name", "agentName": "researcher", "sessionId": s3, "agentId": "d01"},
	))
	e.mustRun(t, "ingest", "--all")
	return e
}

// jsonTags returns the JSON keys of a struct type, embedded structs flattened.
func jsonTags(typ reflect.Type) []string {
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			out = append(out, jsonTags(f.Type)...)
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}

// walk resolves a JSON path such as "", "[]", "events[]", "total" or
// "days[].categories[]" against a decoded value and returns the objects
// it selects.
func walk(t *testing.T, v any, path string) []map[string]any {
	t.Helper()
	if head, rest, ok := strings.Cut(path, "."); ok {
		var out []map[string]any
		for _, obj := range walk(t, v, head) {
			out = append(out, walk(t, obj, rest)...)
		}
		return out
	}
	if path == "" {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("expected an object, got %T", v)
		}
		return []map[string]any{m}
	}
	name, isList := strings.CutSuffix(path, "[]")
	if name != "" {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("expected an object with %q, got %T", name, v)
		}
		v = m[name]
	}
	if !isList {
		return walk(t, v, "")
	}
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("%s: expected an array, got %T", path, v)
	}
	var out []map[string]any
	for _, item := range list {
		out = append(out, walk(t, item, "")...)
	}
	return out
}

func objKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	sort.Strings(s)
	return s
}

// TestSkillSchemaColumns checks every documented table and view against
// PRAGMA table_xinfo of the fixture database: the documented columns are
// exactly the real ones (generated columns included, FTS hidden columns
// excluded).
func TestSkillSchemaColumns(t *testing.T) {
	e := skillEnv(t)
	st, err := store.OpenReadOnly(e.db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	_, rows, err := st.Query(t.Context(), "SELECT name FROM sqlite_master WHERE type IN ('table','view')")
	if err != nil {
		t.Fatal(err)
	}
	objects := map[string]bool{}
	for _, r := range rows {
		objects[r[0].(string)] = true
	}
	sections := skillSections(t)
	for _, must := range []string{"sessions", "events", "events_fts", "exchanges", "daily_activity"} {
		if _, ok := sections[must]; !ok {
			t.Errorf("SKILL.md does not document %q", must)
		}
	}
	for key, documented := range sections {
		if strings.Contains(key, "--json") {
			continue
		}
		t.Run(key, func(t *testing.T) {
			if !objects[key] {
				t.Fatalf("SKILL.md documents %q, which is neither a table nor a view", key)
			}
			_, rows, err := st.Query(t.Context(), "PRAGMA table_xinfo("+key+")")
			if err != nil {
				t.Fatal(err)
			}
			var actual []string
			for _, r := range rows {
				if r[6].(int64) == 1 { // hidden column of a virtual table
					continue
				}
				actual = append(actual, r[1].(string))
			}
			if got, want := sorted(documented), sorted(actual); !reflect.DeepEqual(got, want) {
				t.Errorf("columns of %s\n documented: %v\n schema:     %v", key, got, want)
			}
		})
	}
}

// TestSkillJSONFields checks the documented JSON fields of every read
// command against the real output on the fixture database and against the
// JSON tags of the output types: every object carries exactly the documented
// keys.
func TestSkillJSONFields(t *testing.T) {
	e := skillEnv(t)
	sections := skillSections(t)
	show := []string{"show", skillSession3, "--json", "--all"}
	workday := []string{"stats", "--json", "--workday", "--since", "2026-10-05", "--until", "2026-10-07"}
	worktime := []string{"stats", "--json", "--worktime", "--since", "2026-10-05", "--until", "2026-10-07"}
	tests := []struct {
		key  string
		args []string
		path string
		typ  reflect.Type
	}{
		{"sessions --json", []string{"sessions", "--json", "--subagents", "--all-sessions"}, "[]", reflect.TypeOf(SessionJSON{})},
		{"show --json", show, "", nil},
		{"show --json events[]", show, "events[]", reflect.TypeOf(EventJSON{})},
		{"search --json", []string{"search", "sbom", "--json"}, "[]", reflect.TypeOf(HitJSON{})},
		{"search --by-session --json", []string{"search", "sbom", "--json", "--by-session"}, "[]", reflect.TypeOf(SessionHitsJSON{})},
		{"status --json", []string{"status", "--json"}, "", reflect.TypeOf(StatusJSON{})},
		{"stats --json", []string{"stats", "--json", "--month", "2026-10"}, "", reflect.TypeOf(StatsJSON{})},
		{"stats --json --period", []string{"stats", "--json", "--period", "week", "--by", "project", "--month", "2026-10"}, "", reflect.TypeOf(ActivitySeriesJSON{})},
		{"stats --json --workday", workday, "", reflect.TypeOf(WorkdayJSON{})},
		{"stats --json --workday days[]", workday, "days[]", reflect.TypeOf(WorkdayDayJSON{})},
		{"stats --json --workday total", workday, "total", reflect.TypeOf(WorkdayTotalJSON{})},
		{"stats --json --worktime", worktime, "", reflect.TypeOf(WorktimeJSON{})},
		{"stats --json --worktime days[]", worktime, "days[]", reflect.TypeOf(WorktimeDayJSON{})},
		{"stats --json --worktime days[].categories[]", worktime, "days[].categories[]", reflect.TypeOf(WorktimeCellJSON{})},
		{"stats --json --worktime days[].categories[].projects[]", worktime, "days[].categories[].projects[]", reflect.TypeOf(WorktimeProjectJSON{})},
		{"stats --json --worktime days[].categories[].sessions[]", worktime, "days[].categories[].sessions[]", reflect.TypeOf(WorktimeSessionJSON{})},
		{"stats --json --worktime categories[]", worktime, "categories[]", reflect.TypeOf(WorktimeCategoryTotalJSON{})},
		{"stats --json --worktime total", worktime, "total", reflect.TypeOf(WorktimeTotalJSON{})},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			documented, ok := sections[tt.key]
			if !ok {
				t.Fatalf("SKILL.md has no section %q", tt.key)
			}
			delete(sections, tt.key)
			objects := walk(t, decode[any](t, e.mustRun(t, tt.args...)), tt.path)
			if len(objects) == 0 {
				t.Fatalf("%v: no objects at %q", tt.args, tt.path)
			}
			for _, obj := range objects {
				for k := range obj {
					if !slices.Contains(documented, k) {
						t.Errorf("key %q is in the output but not documented", k)
					}
				}
				for _, d := range documented {
					if _, ok := obj[d]; !ok {
						t.Errorf("documented key %q missing in %v", d, objKeys(obj))
					}
				}
			}
			if tt.typ != nil {
				if got, want := sorted(documented), sorted(jsonTags(tt.typ)); !reflect.DeepEqual(got, want) {
					t.Errorf("documented fields differ from %s\n documented: %v\n type:       %v", tt.typ.Name(), got, want)
				}
			}
		})
	}
	for key := range sections {
		if strings.Contains(key, "--json") {
			t.Errorf("SKILL.md section %q is not covered by this test", key)
		}
	}
}

// TestSkillQueriesRun runs every SQL example of SKILL.md against the fixture
// database. The full-text examples and the exchanges example must return
// rows (the fixture talks about "sbom" and contains the documented session
// id); the time-window example only has to run, because the 'utc' modifier
// depends on the machine's time zone.
func TestSkillQueriesRun(t *testing.T) {
	e := skillEnv(t)
	n := 0
	for _, b := range skillBlocks() {
		var query string
		switch {
		case b.lang == "sh" && strings.HasPrefix(b.body, "total-recall sql --json \""):
			query = strings.TrimSuffix(strings.TrimPrefix(b.body, "total-recall sql --json \""), "\"")
		case b.lang == "sql" && strings.HasPrefix(b.body, "WHERE"):
			query = "SELECT ts FROM events " + b.body
		default:
			continue
		}
		n++
		rows := decode[[]map[string]any](t, e.mustRun(t, "sql", "--json", query))
		mustHaveRows := strings.Contains(query, "MATCH") || strings.Contains(query, "exchanges") || strings.Contains(query, "sqlite_master")
		if mustHaveRows && len(rows) == 0 {
			t.Errorf("no rows for\n%s", query)
		}
	}
	if n < 6 {
		t.Errorf("found only %d SQL examples, expected the four core queries and more", n)
	}
}

// TestSkillOutputContract covers the output contract as the skill documents
// it: JSON errors on stdout, jsonl, --no-truncate.
func TestSkillOutputContract(t *testing.T) {
	e := skillEnv(t)
	t.Run("ErrorOnStdout", func(t *testing.T) {
		tests := []struct {
			name string
			args []string
		}{
			{"sql", []string{"sql", "--json", "SELECT nosuchcol FROM sessions"}},
			{"show", []string{"show", "nosuchsession", "--json"}},
			{"sessions", []string{"sessions", "--json", "--since", "not-a-date"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				r := e.run(t, "", tt.args...)
				if r.code != 1 {
					t.Fatalf("exit %d, want 1", r.code)
				}
				out := decode[map[string]map[string]any](t, r.stdout)
				if out["error"]["kind"] == "" || out["error"]["message"] == "" {
					t.Errorf("error object incomplete: %s", r.stdout)
				}
			})
		}
	})
	t.Run("JSONL", func(t *testing.T) {
		out := e.mustRun(t, "sql", "--format", "jsonl", "SELECT id FROM sessions ORDER BY id")
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if want := e.count(t, "SELECT count(*) FROM sessions"); len(lines) != want {
			t.Fatalf("%d lines, want %d:\n%s", len(lines), want, out)
		}
		for _, l := range lines {
			if _, ok := decode[map[string]any](t, l)["id"]; !ok {
				t.Errorf("line without id: %s", l)
			}
		}
	})
	t.Run("NoTruncate", func(t *testing.T) {
		long := strings.Repeat("x", 300)
		out := e.mustRun(t, "sql", "--no-truncate", "SELECT '"+long+"' AS long")
		if !strings.Contains(out, long) {
			t.Errorf("--no-truncate still shortened the cell:\n%s", out)
		}
	})
}

// TestSkillShowWindow covers the window and paging flags of show and
// search --session as the skill documents them. Times are local
// (Europe/Berlin in the test config): 11:00-12:00 local is 09:00-10:00 UTC.
func TestSkillShowWindow(t *testing.T) {
	e := skillEnv(t)
	s1 := "claude:" + tf.SmallSession1
	events := func(args ...string) []map[string]any {
		t.Helper()
		out := decode[map[string]any](t, e.mustRun(t, append([]string{"show", s1, "--json"}, args...)...))
		return walk(t, out, "events[]")
	}
	if got := events("--since", "2026-10-05T11:00", "--until", "2026-10-05T12:00"); len(got) != 2 {
		t.Errorf("window: %d events, want 2", len(got))
	}
	first := events("--limit", "1")
	second := events("--limit", "1", "--offset", "1")
	if len(first) != 1 || len(second) != 1 || first[0]["id"] == second[0]["id"] {
		t.Errorf("paging: first %v, second %v", first, second)
	}
	for _, b := range skillBlocks() {
		if b.lang == "sh" && strings.HasPrefix(b.body, "total-recall show ") {
			args := strings.Fields(strings.TrimPrefix(b.body, "total-recall "))
			e.mustRun(t, args...)
		}
	}
	hits := decode[[]map[string]any](t, e.mustRun(t, "search", "count", "--json", "--session", tf.SmallSession1[:8]))
	if len(hits) == 0 {
		t.Fatal("search --session: no hits")
	}
	for _, h := range hits {
		if h["session_id"] != s1 {
			t.Errorf("search --session returned a hit from %v", h["session_id"])
		}
	}
}
