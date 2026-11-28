package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Main(&App{Args: args, Stdout: &out, Stderr: &errb})
	return code, out.String(), errb.String()
}

func TestVersionText(t *testing.T) {
	code, out, _ := run(t, "version")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"total-recall-analytics", "DuckDB", "sqlite_scanner", "compiled in"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestVersionJSONHasEveryKey(t *testing.T) {
	code, out, _ := run(t, "version", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, k := range []string{"version", "duckdb", "duckdb_source", "platform", "extensions"} {
		if _, ok := m[k]; !ok {
			t.Errorf("key %q missing: %s", k, out)
		}
	}
	exts, _ := m["extensions"].([]any)
	found := false
	for _, x := range exts {
		e := x.(map[string]any)
		for _, k := range []string{"name", "version", "static"} {
			if _, ok := e[k]; !ok {
				t.Errorf("extension key %q missing: %v", k, e)
			}
		}
		if e["static"] != true {
			t.Errorf("extension not compiled in: %v", e)
		}
		found = found || e["name"] == "sqlite_scanner"
	}
	if !found {
		t.Errorf("sqlite_scanner missing: %v", exts)
	}
}

func TestErrorsAsJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		kind string
	}{
		{"unknown flag", []string{"version", "--json", "--nope"}, "usage"},
		{"unknown command", []string{"nope", "--json"}, "usage"},
		{"extra argument", []string{"version", "x", "--json"}, "usage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := run(t, tc.args...)
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			var e struct {
				Error struct{ Kind, Message string } `json:"error"`
			}
			if err := json.Unmarshal([]byte(out), &e); err != nil {
				t.Fatalf("stdout is not an error object: %v: %q", err, out)
			}
			if e.Error.Kind != tc.kind || e.Error.Message == "" {
				t.Errorf("error = %+v, want kind %s", e.Error, tc.kind)
			}
			if !strings.Contains(errOut, e.Error.Message) {
				t.Errorf("stderr %q lacks the message", errOut)
			}
		})
	}
}

func TestErrorWithoutJSONOnlyOnStderr(t *testing.T) {
	code, out, errOut := run(t, "version", "--nope")
	if code != 1 || out != "" || errOut == "" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}
