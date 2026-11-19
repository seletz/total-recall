package cli

import (
	"sort"
	"strings"
	"testing"
)

// TestWorkdayProjects checks stats --workday --projects against the real
// --json output of the demo archive: the counted events of the projects of
// a day add up to the events of that day, and the totals add up to the
// days.
func TestWorkdayProjects(t *testing.T) {
	t.Parallel()
	e, db := demoProjects(t)
	for _, mode := range []struct {
		name string
		args []string
	}{
		{"human only (the default)", nil},
		{"all events", []string{"--all-events"}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			base := append(append([]string{}, db...), "stats", "--workday", "--since", "2026-10-01", "--until", "2026-10-31", "--json")
			base = append(base, mode.args...)
			plain := decode[WorkdayJSON](t, e.mustRun(t, base...))
			withProjects := decode[WorkdayJSON](t, e.mustRun(t, append(base, "--projects")...))
			raw := decode[map[string]any](t, e.mustRun(t, append(base, "--projects")...))
			if _, ok := raw["projects"].([]any); !ok {
				t.Fatalf("no top-level projects: %v", raw["projects"])
			}
			if len(plain.Days) != len(withProjects.Days) || len(plain.Days) != 31 {
				t.Fatalf("days: %d plain, %d with projects", len(plain.Days), len(withProjects.Days))
			}
			if decode[map[string]any](t, e.mustRun(t, base...))["projects"] != nil {
				t.Error("projects present without --projects")
			}

			sums := map[string]int{}
			total := 0
			active := 0
			for i, d := range withProjects.Days {
				p := plain.Days[i]
				if d.Day != p.Day || d.Human != p.Human || d.Agent != p.Agent || d.SpanSeconds != p.SpanSeconds {
					t.Errorf("--projects changed the day: %+v vs %+v", d, p)
				}
				if d.Projects == nil {
					t.Fatalf("%s: no projects list", d.Day)
				}
				events := 0
				var keys []string
				for _, pr := range *d.Projects {
					events += pr.Events
					sums[pr.Key] += pr.Events
					keys = append(keys, pr.Key)
					if pr.Name == "" || pr.Events <= 0 || pr.Minutes <= 0 || pr.Minutes > pr.Events {
						t.Errorf("%s: project %+v", d.Day, pr)
					}
				}
				if !sort.SliceIsSorted(*d.Projects, func(a, b int) bool {
					x, y := (*d.Projects)[a], (*d.Projects)[b]
					return x.Events > y.Events || x.Events == y.Events && x.Key < y.Key
				}) {
					t.Errorf("%s: projects not by events descending: %v", d.Day, keys)
				}
				// Sum = parts: the projects of a day are all counted events.
				if want := p.Human + p.Agent; events != want {
					t.Errorf("%s: projects add up to %d events, the day has %d", d.Day, events, want)
				}
				total += events
				if events > 0 {
					active++
				}
			}
			if active == 0 {
				t.Fatal("no activity in the range")
			}
			if withProjects.Projects == nil {
				t.Fatal("no totals")
			}
			got := 0
			for _, pr := range *withProjects.Projects {
				got += pr.Events
				if sums[pr.Key] != pr.Events {
					t.Errorf("total of %s = %d, days say %d", pr.Key, pr.Events, sums[pr.Key])
				}
			}
			if got != total || len(*withProjects.Projects) != len(sums) {
				t.Errorf("totals add up to %d events in %d projects, days to %d in %d", got, len(*withProjects.Projects), total, len(sums))
			}
			// Configured projects are named by their definition.
			names := map[string]bool{}
			for _, pr := range *withProjects.Projects {
				names[pr.Name] = true
			}
			if !names["kompass"] || !names["wetterwarte"] {
				t.Errorf("names = %v", names)
			}
		})
	}
}

func TestWorkdayProjectsFlagsAndTable(t *testing.T) {
	t.Parallel()
	e, db := demoProjects(t)
	r := e.run(t, "", append(append([]string{}, db...), "stats", "--projects", "--json")...)
	if r.code != 1 || decode[ErrorJSON](t, r.stdout).Error.Kind != ErrKindUsage || !strings.Contains(r.stdout, "--workday") {
		t.Errorf("--projects without --workday = %+v", r)
	}
	out := e.mustRun(t, append(append([]string{}, db...), "stats", "--workday", "--projects", "--since", "2026-10-26", "--until", "2026-10-30")...)
	if !strings.Contains(out, "PROJECTS") || !strings.Contains(out, "kompass") {
		t.Errorf("table:\n%s", out)
	}
}
