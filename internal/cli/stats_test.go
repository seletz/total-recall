package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	tf "github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// activityRange is the whole Activity fixture (2025-W49 .. 2026-W02).
var activityRange = []string{"--since", "2025-12-01", "--until", "2026-01-11"}

// runActivity runs stats against the ingested Activity fixture.
func runActivity(t *testing.T, e *testEnv, db string, args ...string) result {
	t.Helper()
	return e.run(t, "", append([]string{"--db", db, "stats"}, args...)...)
}

// TestStatsSeries compares the text and JSON output of the period × key
// table with golden files (go test ./internal/cli -run TestStatsSeries -update).
func TestStatsSeries(t *testing.T) {
	tests := []struct {
		name string
		args []string
		home string // App.Home; the fixture's gamma has no repository and shows as its path
	}{
		{"PeriodWeek", []string{"--period", "week"}, ""},
		{"PeriodMonth", []string{"--period", "month"}, ""},
		{"WeekByProject", []string{"--period", "week", "--by", "project"}, ""},
		{"WeekByProjectHome", []string{"--period", "week", "--by", "project"}, "/w"},
		{"WeekByProjectHomeJSON", []string{"--period", "week", "--by", "project", "--json"}, "/w"},
		{"MonthByAgent", []string{"--period", "month", "--by", "agent"}, ""},
		{"DayByDirectoryTop", []string{"--period", "day", "--by", "directory", "--top", "1", "--until", "2025-12-07"}, ""},
		{"ByDefaultsToDay", []string{"--by", "project", "--until", "2025-12-04"}, ""},
		{"EmptyRange", []string{"--period", "week", "--by", "project", "--since", "2026-03-02", "--until", "2026-03-08"}, ""},
		{"PeriodWeekJSON", []string{"--period", "week", "--json"}, ""},
		{"WeekByProjectJSON", []string{"--period", "week", "--by", "project", "--json"}, ""},
		{"MonthByAgentTopJSON", []string{"--period", "month", "--by", "agent", "--top", "1", "--json"}, ""},
		{"EmptyRangeJSON", []string{"--period", "month", "--since", "2026-03-01", "--until", "2026-03-31", "--json"}, ""},
	}
	e := newTestEnv(t)
	_, db := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.args
			if !strings.Contains(strings.Join(args, " "), "--since") {
				args = append(activityRange[:2:2], args...)
			}
			if !strings.Contains(strings.Join(args, " "), "--until") {
				args = append(args, activityRange[2:]...)
			}
			e.home = tt.home
			r := runActivity(t, e, db, args...)
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			golden.RequireEqual(t, []byte(r.stdout))
		})
	}
}

// TestStatsSeriesCounts checks the JSON cells of --period week --by project
// against the documented counts of the Activity fixture. Under the project
// dimension the subagent counts for alpha in 2025-W49.
func TestStatsSeriesCounts(t *testing.T) {
	e := newTestEnv(t)
	_, db := fixturedb.Activity(t)
	r := runActivity(t, e, db, append([]string{"--period", "week", "--by", "project", "--json"}, activityRange...)...)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	out := decode[ActivitySeriesJSON](t, r.stdout)
	if out.Period != "week" || out.By != "project" || out.Timezone != "Europe/Berlin" {
		t.Errorf("header = %+v", out)
	}
	if want := []string{"alpha", "beta", tf.ActivityGamma}; strings.Join(out.Keys, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", out.Keys, want)
	}
	if len(out.Buckets) != 6 {
		t.Fatalf("buckets = %d, want 6", len(out.Buckets))
	}
	names := map[string]string{tf.ActivityAlpha: "alpha", tf.ActivityBeta: "beta", tf.ActivityGamma: tf.ActivityGamma}
	got := map[string]ActivityJSON{} // "week key" -> cell
	for _, b := range out.Buckets {
		for _, c := range b.Cells {
			got[b.Label+" "+c.Key] = c.ActivityJSON
		}
	}
	for _, w := range tf.ActivityWeeks {
		want := ActivityJSON{Sessions: w.Sessions, Human: w.Human, Agent: w.Agent}
		if w.Project == tf.ActivityAlpha && w.Week == "2025-W49" {
			want.Sessions++
			want.Agent += tf.ActivitySubagentCount.Agent
		}
		k := w.Week + " " + names[w.Project]
		if got[k] != want {
			t.Errorf("%s = %+v, want %+v", k, got[k], want)
		}
		delete(got, k)
	}
	for k, c := range got {
		t.Errorf("unexpected cell %s = %+v", k, c)
	}
}

func TestStatsSeriesErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"bad period", []string{"--period", "year"}, "invalid period"},
		{"bad dimension", []string{"--period", "week", "--by", "repo"}, "invalid dimension"},
		{"top without by", []string{"--period", "week", "--top", "3"}, "--top requires --by"},
		{"negative top", []string{"--by", "project", "--top", "-1"}, "--top"},
	}
	e := newTestEnv(t)
	_, db := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runActivity(t, e, db, tt.args...)
			if r.code == 0 || !strings.Contains(r.stderr, tt.want) {
				t.Errorf("exit %d, stderr %q, want %q", r.code, r.stderr, tt.want)
			}
		})
	}
}
