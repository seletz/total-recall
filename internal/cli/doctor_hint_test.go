package cli

import (
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// TestDoctorSetupHint checks the hint about projects with recent activity
// that have no [[project]] or no category: in the text on stdout, with
// --json on stderr, and never a change of the exit code or of the JSON.
func TestDoctorSetupHint(t *testing.T) {
	_, db := fixturedb.Demo(t)
	const projects = "\n[[project]]\nname = \"Kompass\"\nmatch = [\"https://git.invalid/mara/kompass\"]\n"
	tests := []struct {
		name, config string
		args         []string
		// want maps a project to the words its hint line must have; none:
		// there is no hint at all.
		want   map[string][]string
		absent []string
	}{
		{
			name: "no project and no category", config: projects + demoCategories,
			want: map[string][]string{
				"wetterwarte": {"no [[project]]", "no category"},
				"dotfiles":    {"no [[project]]"}, // has the category privat
			},
			absent: []string{"kompass"},
		},
		{
			name: "projects only: category is not checked", config: projects,
			want: map[string][]string{"wetterwarte": {"no [[project]]"}}, absent: []string{"kompass", "no category"},
		},
		{
			name: "categories only: projects are not checked", config: demoCategories,
			want: map[string][]string{"wetterwarte": {"no category"}}, absent: []string{"kompass", "dotfiles", "no [[project]]"},
		},
		{name: "neither configured: no hint", config: ""},
		{name: "days 0 switches it off", config: projects + demoCategories, args: []string{"--days", "0"}},
		{name: "outside the window", config: projects + demoCategories, args: []string{"--days", "1"}},
		{name: "json goes to stderr", config: projects + demoCategories, args: []string{"--json"}, want: map[string][]string{"wetterwarte": {"no category"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			e.now = demo.Now
			e.home = demo.Home
			appendConfig(t, e, tt.config)
			r := e.run(t, "", append([]string{"--db", db, "doctor"}, tt.args...)...)
			if r.code != 0 {
				t.Fatalf("exit %d, want 0 (a hint is no finding)\n%s%s", r.code, r.stdout, r.stderr)
			}
			json := len(tt.args) > 0 && tt.args[0] == "--json"
			hint, other := r.stdout, r.stderr
			if json {
				hint, other = r.stderr, r.stdout
			}
			if strings.Contains(other, "[[project]]") {
				t.Errorf("hint on the wrong stream: %q", other)
			}
			if tt.want == nil {
				if strings.Contains(hint, "last ") {
					t.Errorf("unexpected hint: %q", hint)
				}
				return
			}
			for name, words := range tt.want {
				line := ""
				for _, l := range strings.Split(hint, "\n") {
					if strings.Contains(l, name) {
						line = l
					}
				}
				if line == "" {
					t.Errorf("no line for %s in %q", name, hint)
				}
				for _, w := range words {
					if !strings.Contains(line, w) {
						t.Errorf("%s: line %q lacks %q", name, line, w)
					}
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(strings.ToLower(hint), a) {
					t.Errorf("hint mentions %q: %q", a, hint)
				}
			}
		})
	}
}
