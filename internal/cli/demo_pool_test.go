package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/demo"
)

// TestDemoPoolSumsToAll: the statistics of the hosts of the demo pool add
// up to those of the reference archive with every host, number by number:
// the activity tables, the tokens per day and model, and the tool calls.
// Only work time is not additive (gaps are credited per person), so it is
// not compared.
func TestDemoPoolSumsToAll(t *testing.T) {
	t.Parallel()
	p, err := demo.BuildPool(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	for _, extra := range [][]string{nil, {"--tools"}} {
		args := append([]string{"stats", "--json", "--since", "2026-01-01", "--until", "2026-12-31"}, extra...)
		flat := func(db string) map[string]float64 {
			var v any
			if err := json.Unmarshal([]byte(e.mustRun(t, append([]string{"--db", db}, args...)...)), &v); err != nil {
				t.Fatal(err)
			}
			out := map[string]float64{}
			flatten("", v, out)
			return out
		}
		want := flat(p.All)
		got := map[string]float64{}
		for _, h := range p.Hosts {
			for k, n := range flat(h.Path) {
				got[k] += n
			}
		}
		var diff []string
		for k := range union(want, got) {
			if want[k] != got[k] {
				diff = append(diff, fmt.Sprintf("%s: hosts %v, all %v", k, got[k], want[k]))
			}
		}
		sort.Strings(diff)
		if len(diff) > 0 {
			t.Errorf("stats %v: %d numbers differ, first: %s", extra, len(diff), strings.Join(diff[:min(len(diff), 5)], "; "))
		}
		if want["tokens.output"] == 0 && extra == nil {
			t.Error("the pool has no tokens")
		}
	}
}

// idFields are numeric fields that name a list element rather than count.
var idFields = map[string]bool{"hour": true, "weekday": true}

// flatten writes every number below v into out under its path; the
// elements of a list are named by their non-numeric fields and idFields,
// so the same day, hour, repository or model lines up across archives.
func flatten(path string, v any, out map[string]float64) {
	switch v := v.(type) {
	case float64:
		out[path] += v
	case map[string]any:
		for k, x := range v {
			if _, num := x.(float64); num && idFields[k] {
				continue
			}
			flatten(join(path, k), x, out)
		}
	case []any:
		for _, x := range v {
			flatten(join(path, "["+identity(x)+"]"), x, out)
		}
	}
}

func identity(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Sprint(v)
	}
	var parts []string
	for k, x := range m {
		switch x.(type) {
		case map[string]any, []any:
		case float64:
			if idFields[k] {
				parts = append(parts, fmt.Sprintf("%s=%v", k, x))
			}
		default:
			parts = append(parts, fmt.Sprintf("%s=%v", k, x))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func join(a, b string) string {
	if a == "" {
		return b
	}
	return a + "." + b
}

func union(a, b map[string]float64) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}
