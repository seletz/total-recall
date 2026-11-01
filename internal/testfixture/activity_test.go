package testfixture

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// readLines decodes every JSONL record below projects together with the
// relative path of its file.
func readLines(t *testing.T, projects string) []struct {
	Rel  string
	Line Line
} {
	t.Helper()
	var out []struct {
		Rel  string
		Line Line
	}
	err := filepath.Walk(projects, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		rel, _ := filepath.Rel(projects, p)
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var l Line
			if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
				t.Fatalf("%s: %v", rel, err)
			}
			out = append(out, struct {
				Rel  string
				Line Line
			}{filepath.ToSlash(rel), l})
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestActivityLayout(t *testing.T) {
	projects := Activity(t, t.TempDir())
	lines := readLines(t, projects)
	if len(lines) == 0 {
		t.Fatal("fixture wrote no records")
	}

	dirs := map[string]bool{}
	cwds := map[string]bool{}
	subagents := 0
	var first, last time.Time
	weekdays := map[time.Weekday]bool{}
	hours := map[int]bool{}
	for _, l := range lines {
		dirs[strings.SplitN(l.Rel, "/", 2)[0]] = true
		if strings.Contains(l.Rel, "/subagents/") && strings.HasSuffix(l.Rel, ".jsonl") {
			subagents++
		}
		if cwd, ok := l.Line["cwd"].(string); ok {
			cwds[cwd] = true
		}
		tsv, ok := l.Line["timestamp"].(string)
		if !ok {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, tsv)
		if err != nil {
			t.Fatalf("%s: bad timestamp %q: %v", l.Rel, tsv, err)
		}
		lt := ts.In(ActivityLocation)
		weekdays[lt.Weekday()] = true
		hours[lt.Hour()] = true
		if first.IsZero() || ts.Before(first) {
			first = ts
		}
		if ts.After(last) {
			last = ts
		}
	}

	wantDirs := []string{"-w-alpha", "-w-alpha--worktrees-feature", "-w-beta", "-w-gamma"}
	var gotDirs []string
	for d := range dirs {
		gotDirs = append(gotDirs, d)
	}
	sort.Strings(gotDirs)
	if strings.Join(gotDirs, ",") != strings.Join(wantDirs, ",") {
		t.Errorf("project dirs = %v, want %v", gotDirs, wantDirs)
	}
	for _, cwd := range []string{ActivityAlpha, ActivityAlphaWorktree, ActivityBeta, ActivityGamma} {
		if !cwds[cwd] {
			t.Errorf("no record with cwd %q", cwd)
		}
	}
	if subagents == 0 {
		t.Error("no subagent transcript")
	}
	if span := last.Sub(first); span < 5*7*24*time.Hour || span > 6*7*24*time.Hour {
		t.Errorf("events span %v (%s .. %s), want between five and six weeks", span, first, last)
	}
	if first.Year() == last.Year() {
		t.Errorf("events do not cross a year boundary: %s .. %s", first, last)
	}
	if len(weekdays) < 6 {
		t.Errorf("events on %d weekdays, want at least 6", len(weekdays))
	}
	if len(hours) < 8 {
		t.Errorf("events in %d distinct hours, want at least 8", len(hours))
	}
	for _, cwd := range []string{ActivityAlpha, ActivityAlphaWorktree, ActivityBeta} {
		if _, ok := ActivityGit()[cwd]; !ok {
			t.Errorf("ActivityGit has no entry for %q", cwd)
		}
	}
	if _, ok := ActivityGit()[ActivityGamma]; ok {
		t.Errorf("ActivityGit resolves %q, want no repository", ActivityGamma)
	}
}

// TestActivityWeeksSum checks that the documented per-week table is
// internally consistent with the documented totals.
func TestActivityWeeksSum(t *testing.T) {
	sum := map[string]ActivityCount{}
	for _, w := range ActivityWeeks {
		c := sum[w.Project]
		c.Human += w.Human
		c.Agent += w.Agent
		c.Tool += w.Tool
		sum[w.Project] = c
	}
	for _, tt := range ActivityTotals {
		got := sum[tt.Project]
		if got.Human != tt.Human || got.Agent != tt.Agent || got.Tool != tt.Tool {
			t.Errorf("%s: weeks sum to human=%d agent=%d tool=%d, totals say human=%d agent=%d tool=%d",
				tt.Project, got.Human, got.Agent, got.Tool, tt.Human, tt.Agent, tt.Tool)
		}
	}
}
