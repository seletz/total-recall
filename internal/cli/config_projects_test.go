package cli

import (
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// demoProjects is a test installation on the demo archive with three
// project definitions (home /home/mara, as in the archive).
func demoProjects(t *testing.T) (*testEnv, []string) {
	t.Helper()
	e := newTestEnv(t)
	e.home = "/home/mara"
	_, db := fixturedb.Demo(t)
	for _, a := range [][]string{
		{"kompass", "--match", "https://git.invalid/mara/kompass*", "--match", "~/src/kompass*", "--description", "Wanderkarten", "--color", "blue"},
		{"wetterwarte", "--match", "https://git.invalid/mara/wetterwarte"},
		{"fahrtenbuch", "--match", "https://git.invalid/mara/fahrtenbuch", "--color", "teal"},
	} {
		e.mustRun(t, append([]string{"config", "project", "add"}, a...)...)
	}
	return e, []string{"--db", db}
}

func TestConfigProjectsJSON(t *testing.T) {
	t.Parallel()
	e, db := demoProjects(t)
	out := e.mustRun(t, append(db, "config", "projects", "--json")...)
	raw := decode[map[string]any](t, out)
	for _, k := range []string{"definitions", "keys", "unassigned", "totals", "problem", "preview"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("key %q missing", k)
		}
	}
	if raw["preview"] != nil || raw["problem"] != nil {
		t.Errorf("preview = %v, problem = %v, want null", raw["preview"], raw["problem"])
	}
	c := decode[ConfigProjectsJSON](t, out)

	// Sum = parts, against the real archive: the sessions of the
	// definitions plus the ones without a definition are all main
	// sessions with a directory, and so are the keys.
	rows := decode[[]struct {
		N int `json:"n"`
	}](t, e.mustRun(t, append(db, "sql", "--json", "select count(*) as n from sessions where parent_id is null and coalesce(nullif(git_root,''), cwd, '') != ''")...))
	want := rows[0].N
	sessions, paths := c.Unassigned.Sessions, c.Unassigned.Paths
	for _, d := range c.Definitions {
		sessions += d.Sessions
		paths += d.Paths
	}
	keySessions := 0
	assigned := map[string]int{}
	for _, k := range c.Keys {
		keySessions += k.Sessions
		if k.Project != "" {
			assigned[string(k.Project)] += k.Sessions
		}
	}
	if sessions != want || keySessions != want || c.Totals.Sessions != want || paths != len(c.Keys) || c.Totals.Paths != len(c.Keys) {
		t.Errorf("sessions: definitions+unassigned %d, keys %d, totals %d, archive %d; paths %d+%d/%d", sessions, keySessions, c.Totals.Sessions, want, paths, c.Totals.Paths, len(c.Keys))
	}
	if want == 0 || len(c.Definitions) != 3 || c.Unassigned.Sessions == 0 {
		t.Fatalf("demo archive: %d sessions, %d definitions, unassigned %+v", want, len(c.Definitions), c.Unassigned)
	}
	for _, d := range c.Definitions {
		if d.Sessions != assigned[d.Name] {
			t.Errorf("%s: %d sessions, keys say %d", d.Name, d.Sessions, assigned[d.Name])
		}
	}
	if k := c.Definitions[0]; k.Name != "kompass" || k.Position != 1 || k.Description != "Wanderkarten" || k.Sessions != 326 || k.Paths != 1 {
		t.Errorf("kompass = %+v", k)
	}
	// Unassigned keys have no project; the others name theirs.
	for _, k := range c.Keys {
		if k.Name == "" || k.Key == "" || k.Last == "" {
			t.Errorf("incomplete key %+v", k)
		}
	}
	if c.Keys[0].Key != "/home/mara/src/kompass" || c.Keys[0].Project != "kompass" || c.Keys[0].Remote == "" {
		t.Errorf("first key = %+v", c.Keys[0])
	}
}

func TestConfigProjectsPreview(t *testing.T) {
	t.Parallel()
	e, db := demoProjects(t)
	before := cfgText(t, e)
	tests := []struct {
		name         string
		args         []string
		paths, sess  int
		replaces     bool
		takenByFirst string // taken_by of the first match
		text         string
	}{
		{"new definition on a path", []string{"--name", "notizen", "--match", "~/notizen"}, 1, 58, false, "", "matches 1 path · 58 sessions: ~/notizen"},
		{"nothing matches", []string{"--match", "~/nirgends"}, 0, 0, false, "", "matches nothing in the archive"},
		{"taken by an earlier definition", []string{"--name", "src", "--match", "~/src/*"}, 0, 0, false, "kompass", "goes to kompass (earlier)"},
		{"edit replaces the definition of that name", []string{"--name", "wetterwarte", "--match", "~/src/wetterwarte"}, 1, 218, true, "", "matches 1 path · 218 sessions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append(append([]string{}, db...), "config", "projects", "--json", "--preview")
			c := decode[ConfigProjectsJSON](t, e.mustRun(t, append(args, tt.args...)...))
			p := c.Preview
			if p == nil {
				t.Fatal("no preview")
			}
			if p.Sessions != tt.sess || p.Paths != tt.paths || p.Replaces != tt.replaces || !strings.Contains(p.Text, tt.text) {
				t.Errorf("preview = %+v", p)
			}
			if tt.takenByFirst != "" {
				found := false
				for _, m := range p.Matches {
					found = found || string(m.TakenBy) == tt.takenByFirst
				}
				if !found {
					t.Errorf("no match taken by %s: %+v", tt.takenByFirst, p.Matches)
				}
			}
			// Sum = parts: matches the draft gets plus the ones an earlier
			// definition takes are all the keys the patterns match.
			mine, taken := 0, 0
			for _, m := range p.Matches {
				if m.TakenBy == "" {
					mine += m.Sessions
				} else {
					taken += m.Sessions
				}
			}
			if mine != p.Sessions || mine+taken != p.MatchedSessions {
				t.Errorf("mine %d + taken %d vs %d / %d", mine, taken, p.Sessions, p.MatchedSessions)
			}
		})
	}
	if cfgText(t, e) != before {
		t.Error("--preview wrote the config")
	}
	for _, tt := range []struct {
		args []string
		kind string
	}{
		{[]string{"--preview"}, ErrKindUsage},                     // no pattern
		{[]string{"--preview", "--match", ""}, ErrKindValidation}, // empty pattern
		{[]string{"--preview", "--match", "x", "--color", "puce"}, ErrKindValidation},
		{[]string{"--match", "x"}, ErrKindUsage}, // draft flags without --preview
	} {
		r := e.run(t, "", append(append(append([]string{}, db...), "config", "projects", "--json"), tt.args...)...)
		if r.code != 1 || decode[ErrorJSON](t, r.stdout).Error.Kind != tt.kind {
			t.Errorf("%v: %+v", tt.args, r)
		}
	}
}

func TestConfigProjectsNoDefinitionsAndTable(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	e.home = "/home/mara"
	_, db := fixturedb.Demo(t)
	c := decode[ConfigProjectsJSON](t, e.mustRun(t, "--db", db, "config", "projects", "--json"))
	if len(c.Definitions) != 0 || c.Unassigned.Sessions != c.Totals.Sessions || c.Totals.Sessions == 0 {
		t.Errorf("without definitions: %+v / %+v", c.Unassigned, c.Totals)
	}
	out := e.mustRun(t, "--db", db, "config", "projects")
	if !strings.Contains(out, "/home/mara/src/kompass") && !strings.Contains(out, "kompass") {
		t.Errorf("table = %q", out)
	}
}
