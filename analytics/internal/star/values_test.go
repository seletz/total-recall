package star

import (
	"context"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

// TestValueSetsCoverTheModel: every value ingest can write is in the
// accepted set of its column.
func TestValueSetsCoverTheModel(t *testing.T) {
	want := map[string][]string{
		"kind":       {string(model.KindMessage), string(model.KindToolUse), string(model.KindToolResult), string(model.KindSystem), string(model.KindMeta)},
		"actor":      {string(model.ActorHuman), string(model.ActorAgent), string(model.ActorPeer), string(model.ActorAutomation), string(model.ActorTool), ""},
		"input_kind": {string(model.InputTyped), string(model.InputPasted), string(model.InputBash), string(model.InputRemote), ""},
	}
	got := map[string][]string{}
	for _, s := range valueSets {
		got[s.Column] = s.Values
		for _, v := range s.Values {
			if v != "" && !isValid(s.Column, v) {
				t.Errorf("%s: %q is not a valid model value", s.Column, v)
			}
		}
	}
	for col, vals := range want {
		if !reflect.DeepEqual(sorted(got[col]), sorted(vals)) {
			t.Errorf("%s: value set %q, want %q", col, got[col], vals)
		}
	}
}

func isValid(col, v string) bool {
	switch col {
	case "kind":
		return model.Kind(v).Valid()
	case "actor":
		return model.Actor(v).Valid()
	case "input_kind":
		return model.InputKind(v).Valid()
	}
	return false
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

// TestValueSetsMatchTheSQL: the staging model maps exactly the value sets
// of Go, and the data test accepts them plus "unknown".
func TestValueSetsMatchTheSQL(t *testing.T) {
	stg := mustRead(t, "models/staging/stg_event.sql")
	test := mustRead(t, "tests/accepted_values.sql")
	for _, s := range valueSets {
		reStg := regexp.MustCompile(`COALESCE\(e\.` + s.Column + `, ''\) IN \(([^)]*)\) THEN`)
		m := reStg.FindStringSubmatch(stg)
		if m == nil {
			t.Errorf("stg_event.sql: no mapping of %s", s.Column)
		} else if got := sqlList(m[1]); !reflect.DeepEqual(sorted(got), sorted(s.Values)) {
			t.Errorf("stg_event.sql %s: %q, want %q", s.Column, got, s.Values)
		}
		reTest := regexp.MustCompile(`WHERE ` + s.Column + ` NOT IN \(([^)]*)\)`)
		m = reTest.FindStringSubmatch(test)
		if m == nil {
			t.Errorf("accepted_values.sql: no test of %s", s.Column)
		} else if got := sqlList(m[1]); !reflect.DeepEqual(sorted(got), sorted(append(slices.Clone(s.Values), Unknown))) {
			t.Errorf("accepted_values.sql %s: %q, want %q plus %q", s.Column, got, s.Values, Unknown)
		}
	}
}

// sqlList parses "'a', 'b', ''".
func sqlList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		out = append(out, strings.Trim(strings.TrimSpace(p), "'"))
	}
	return out
}

// TestUnknownValuesWarn: values of the full schema range load as they are;
// a value outside it (a newer total-recall) loads as "unknown" and is
// reported, it does not fail the build.
func TestUnknownValuesWarn(t *testing.T) {
	archive := demoCopy(t)
	execSQLite(t, archive,
		"UPDATE events SET kind = 'system' WHERE seq IN (SELECT seq FROM events WHERE kind = 'message' ORDER BY seq LIMIT 2)",
		"UPDATE events SET kind = 'meta' WHERE seq IN (SELECT seq FROM events WHERE kind = 'message' ORDER BY seq LIMIT 1)",
		"UPDATE events SET kind = 'future-kind' WHERE seq IN (SELECT seq FROM events WHERE kind = 'message' ORDER BY seq LIMIT 2)",
		"UPDATE events SET actor = 'future-actor' WHERE seq IN (SELECT seq FROM events WHERE actor = 'tool' ORDER BY seq LIMIT 1)",
		"UPDATE events SET input_kind = 'future-input' WHERE seq IN (SELECT seq FROM events WHERE actor = 'human' ORDER BY seq LIMIT 3)",
	)
	db := openDuck(t, filepath.Join(t.TempDir(), "a.duckdb"))
	r := mustRun(t, db, opts(t, archive))
	want := []UnknownValue{
		{Column: "actor", Value: "future-actor", Events: 1},
		{Column: "input_kind", Value: "future-input", Events: 3},
		{Column: "kind", Value: "future-kind", Events: 2},
	}
	if !reflect.DeepEqual(r.Unknown, want) {
		t.Errorf("Unknown %+v, want %+v", r.Unknown, want)
	}
	for q, want := range map[string]string{
		"SELECT count(*) FROM analytics_events_v1 WHERE kind = 'system'":       "2",
		"SELECT count(*) FROM analytics_events_v1 WHERE kind = 'meta'":         "1",
		"SELECT count(*) FROM analytics_events_v1 WHERE kind = 'unknown'":      "2",
		"SELECT count(*) FROM analytics_events_v1 WHERE actor = 'unknown'":     "1",
		"SELECT count(*) FROM analytics_events_v1 WHERE input_kind = 'unknown'": "3",
		"SELECT count(*) FROM analytics_events_v1":                             sqliteScalar(t, archive, "SELECT count(*) FROM events"),
	} {
		if got := row(t, db, q); got != want {
			t.Errorf("%s = %s, want %s", q, got, want)
		}
	}
	// an incremental run warns only about new events
	again := mustRun(t, db, opts(t, archive))
	if len(again.Unknown) != 0 {
		t.Errorf("second run Unknown %+v, want none", again.Unknown)
	}
}

// TestFailuresNameTheRows: a failing data test reports its first rows, so
// the message says which column and value broke it.
func TestFailuresNameTheRows(t *testing.T) {
	db := openDuck(t, filepath.Join(t.TempDir(), "a.duckdb"))
	m := Model{Name: "accepted_values", Text: "SELECT 'kind' AS col, 'x' || i AS value FROM range(5) t(i)"}
	n, rows, err := TestFailures(context.Background(), db, m, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("failures %d, want 5", n)
	}
	want := []string{"col=kind value=x0", "col=kind value=x1", "col=kind value=x2"}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows %q, want %q", rows, want)
	}
}
