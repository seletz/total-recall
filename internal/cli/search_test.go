package cli

import (
	"encoding/json"
	"strings"
	"testing"

	tf "github.com/seletz/total-recall/internal/testfixture"
)

// typedHit decodes any typed search hit for checks across types.
type typedHit map[string]any

func (h typedHit) s(key string) string {
	v, _ := h[key].(string)
	return v
}

func TestSearchTypeJSON(t *testing.T) {
	e := memoryEnv(t)
	beta := "claude:" + tf.SmallSession2
	tests := []struct {
		name  string
		args  []string
		check func(t *testing.T, hits []typedHit)
	}{
		{"default is event with type and project", []string{"release", "notes"}, func(t *testing.T, hits []typedHit) {
			if len(hits) != 2 {
				t.Fatalf("hits = %+v", hits)
			}
			for _, h := range hits {
				if h.s("type") != "event" || h.s("project") != "/w/beta" || h.s("event_id") == "" || h.s("kind") == "" {
					t.Errorf("event hit = %+v", h)
				}
			}
		}},
		{"session", []string{"--type", "session", "beta"}, func(t *testing.T, hits []typedHit) {
			if len(hits) != 1 {
				t.Fatalf("hits = %+v", hits)
			}
			h := hits[0]
			if h.s("type") != "session" || h.s("session_id") != beta || h.s("session_name") != "Beta work" || h.s("snippet") != "[Beta] work" ||
				h.s("project") != "/w/beta" || h.s("harness") != "claude" || h.s("ts") != "2026-10-06T14:00:00.000Z" ||
				h.s("ended_at") != "2026-10-06T14:05:00.000Z" || h["events"] != float64(2) || h["rank"] != float64(0) {
				t.Errorf("session hit = %+v", h)
			}
		}},
		{"memory", []string{"--type", "memory", "beacon"}, func(t *testing.T, hits []typedHit) {
			if len(hits) != 1 {
				t.Fatalf("hits = %+v", hits)
			}
			h := hits[0]
			if h.s("type") != "memory" || h.s("project") != "/w/alpha" || h.s("project_key") != "-w-alpha" || h.s("path") != "lighthouse.md" ||
				h.s("sha256") == "" || h.s("ts") != "2026-10-01T09:00:00.000Z" || !strings.Contains(h.s("snippet"), "[beacon]") ||
				h["session_id"] != nil || h["session_name"] != nil || h.s("repo") != "/w/alpha" {
				t.Errorf("memory hit = %+v", h)
			}
			for _, k := range []string{"session_id", "session_name", "rank"} {
				if _, ok := h[k]; !ok {
					t.Errorf("memory hit lacks %s", k)
				}
			}
		}},
		{"project scopes every type", []string{"--type", "event,session,memory", "--project", "beta", "memory"}, func(t *testing.T, hits []typedHit) {
			if len(hits) != 1 || hits[0].s("type") != "memory" || hits[0].s("project") != "/w/beta" {
				t.Errorf("hits = %+v", hits)
			}
		}},
		{"project excludes events", []string{"--project", "/w/alpha", "release"}, func(t *testing.T, hits []typedHit) {
			if len(hits) != 0 {
				t.Errorf("hits = %+v", hits)
			}
		}},
		{"limit per type", []string{"--type", "event,memory", "--raw", "--limit", "1", "files OR memory"}, func(t *testing.T, hits []typedHit) {
			if len(hits) != 2 || hits[0].s("type") != "event" || hits[1].s("type") != "memory" {
				t.Errorf("hits = %+v", hits)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"search", "--json"}, tt.args...)
			hits := decode[[]typedHit](t, e.mustRun(t, args...))
			if tt.check != nil {
				tt.check(t, hits)
			}
		})
	}
	// Groups come in the order of --type.
	hits := decode[[]typedHit](t, e.mustRun(t, "search", "--json", "--type", "memory,session,event", "beta"))
	var types []string
	for _, h := range hits {
		types = append(types, h.s("type"))
	}
	if strings.Join(types, ",") != "memory,session" {
		t.Errorf("types = %v", types)
	}
	// jsonl: one hit per line, each with its type.
	out := e.mustRun(t, "search", "--format", "jsonl", "--type", "session,memory", "beta")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("jsonl = %q", out)
	}
	var first typedHit
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil || first.s("type") != "session" {
		t.Errorf("jsonl line = %q, %v", lines[0], err)
	}
}

func TestSearchTypeConfiguredProject(t *testing.T) {
	e := memoryEnv(t)
	e.appendConfig(t, "\n[[project]]\nname = \"leuchtturm\"\nmatch = [\"/w/alpha\"]\n")
	hits := decode[[]typedHit](t, e.mustRun(t, "search", "--json", "--type", "memory,event", "--project", "leuchtturm", "--raw", "beacon OR files"))
	if len(hits) != 3 {
		t.Fatalf("hits = %+v", hits)
	}
	for _, h := range hits {
		if h.s("project") != "leuchtturm" {
			t.Errorf("hit project = %+v", h)
		}
	}
}

func TestSearchTypeErrors(t *testing.T) {
	e := memoryEnv(t)
	tests := []struct {
		name string
		args []string
		kind string
		msg  string
	}{
		{"unknown type", []string{"--type", "bogus"}, ErrKindUsage, `unknown type "bogus"`},
		{"reserved type", []string{"--type", "event,file"}, ErrKindUsage, `type "file" is not searchable yet`},
		{"actor on memory", []string{"--type", "memory", "--actor", "human"}, ErrKindUsage, "--actor does not apply to type memory"},
		{"raw on session", []string{"--type", "event,session", "--raw"}, ErrKindUsage, "--raw does not apply to type session"},
		{"name on memory", []string{"--type", "memory", "--name", "x"}, ErrKindUsage, "--name does not apply to type memory"},
		{"session on memory", []string{"--type", "memory", "--session", tf.SmallSession2}, ErrKindUsage, "--session does not apply to type memory"},
		{"by-session on memory", []string{"--type", "memory", "--by-session"}, ErrKindUsage, "--by-session applies to type event only"},
		{"unknown project", []string{"--project", "nowhere"}, ErrKindNotFound, `no project matches "nowhere"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"search", "--json", "beta"}, tt.args...)
			r := e.run(t, "", args...)
			var got struct {
				Error struct{ Kind, Message string }
			}
			if err := json.Unmarshal([]byte(r.stdout), &got); err != nil || r.code == 0 {
				t.Fatalf("run = %+v", r)
			}
			if got.Error.Kind != tt.kind || !strings.Contains(got.Error.Message, tt.msg) {
				t.Errorf("error = %+v, want %s %q", got.Error, tt.kind, tt.msg)
			}
		})
	}
}

func TestSearchTypeText(t *testing.T) {
	e := memoryEnv(t)
	r := e.run(t, "", "search", "--type", "session,memory", "beta")
	for _, want := range []string{"session  ", "Beta work", "[Beta] work", "memory  ", "/w/beta", "MEMORY.md", "[beta]"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("text lacks %q:\n%s", want, r.stdout)
		}
	}
	// The default keeps the event lines without a type column.
	if r := e.run(t, "", "search", "release"); strings.HasPrefix(r.stdout, "event") || !strings.Contains(r.stdout, "Beta work") {
		t.Errorf("event text = %q", r.stdout)
	}
	// A limit that cuts hits off says how many there are.
	r = e.run(t, "", "search", "files", "--limit", "1")
	if strings.Count(r.stdout, "\n    ") != 1 || !strings.Contains(r.stderr, "event: 1 of 2 hits") {
		t.Errorf("limit note: stdout %q stderr %q", r.stdout, r.stderr)
	}
	if r := e.run(t, "", "search", "files"); strings.Contains(r.stderr, " of ") {
		t.Errorf("note without a cut: %q", r.stderr)
	}
	if r := e.run(t, "", "search", "--type", "memory", "nothing-like-this"); !strings.Contains(r.stderr, "no matches") {
		t.Errorf("no matches: %q", r.stderr)
	}
}
