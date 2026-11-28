package star

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// TestDbtProjectMatchesMacros: the on-run-start hooks of dbt_project.yml are
// the statements of macros.sql, so dbt and the runner build with the same
// macros.
func TestDbtProjectMatchesMacros(t *testing.T) {
	yml := mustRead(t, "dbt_project.yml")
	var hooks []string
	in := false
	for _, line := range strings.Split(yml, "\n") {
		switch {
		case strings.HasPrefix(line, "on-run-start:"):
			in = true
		case in && strings.HasPrefix(line, "  - \""):
			h := strings.TrimSuffix(strings.TrimPrefix(line, "  - \""), "\"")
			hooks = append(hooks, strings.ReplaceAll(h, `\"`, `"`))
		case in && strings.TrimSpace(line) != "":
			in = false
		}
	}
	var want []string
	for _, s := range splitSQL(mustRead(t, "macros.sql")) {
		var code []string
		for _, l := range strings.Split(s, "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "--") {
				code = append(code, l)
			}
		}
		want = append(want, strings.TrimSuffix(strings.Join(code, " "), ";"))
	}
	if strings.Join(hooks, "\n") != strings.Join(want, "\n") {
		t.Errorf("dbt_project.yml on-run-start differs from macros.sql:\n%s\nwant\n%s", strings.Join(hooks, "\n"), strings.Join(want, "\n"))
	}
}

// TestModelsUseOnlyTheSubset: every model and test renders with the
// runner's subset, and every source() names a table of sources.yml.
func TestModelsUseOnlyTheSubset(t *testing.T) {
	p, err := LoadProject(ProjectFS(), seeds)
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{}
	for _, v := range []string{"seq_from", "batch_id", "source_name", "host", "home", "tz", "zones"} {
		vars[v] = "x"
	}
	srcYml := mustRead(t, "models/sources.yml")
	reSrc := regexp.MustCompile(`source\(\s*'tr'\s*,\s*'(\w+)'\s*\)`)
	for _, m := range append(p.Models, p.Tests...) {
		if m.Materialized == Seed {
			if _, err := fs.Stat(ProjectFS(), "seeds/"+m.Name+".csv"); err != nil {
				t.Errorf("seed %s has no seeds/%s.csv for dbt", m.Name, m.Name)
			}
			continue
		}
		if _, err := Render(m.Text, RenderContext{Sources: map[string]string{"tr": "src"}, Vars: vars}); err != nil {
			t.Errorf("%s: %v", m.Path, err)
		}
		for _, s := range reSrc.FindAllStringSubmatch(m.Text, -1) {
			if !strings.Contains(srcYml, "- name: "+s[1]) {
				t.Errorf("%s: source table %s not in sources.yml", m.Path, s[1])
			}
		}
	}
}
