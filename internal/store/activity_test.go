package store

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/model"
)

// seedActivity writes four sessions: a main session with a subagent in
// /w/app, a session without git in /w/docs and a session of another harness
// in /w/tool. Times are UTC; Europe/Berlin is UTC+1 in winter.
func seedActivity(t *testing.T, s *Store) {
	t.Helper()
	sessions := []model.Session{
		{ID: "claude:p1", Harness: "claude", NativeID: "p1", Cwd: "/w/app", GitRoot: "/w/app",
			StartedAt: ts("2026-10-31T22:00:00Z"), EndedAt: ts("2026-10-31T23:30:00Z")},
		{ID: "claude:p1sub", Harness: "claude", NativeID: "p1sub", ParentID: "claude:p1", Cwd: "/w/app/sub",
			StartedAt: ts("2026-10-31T22:05:00Z"), EndedAt: ts("2026-10-31T22:06:00Z")},
		{ID: "claude:p2", Harness: "claude", NativeID: "p2", Cwd: "/w/docs",
			StartedAt: ts("2025-12-28T12:00:00Z"), EndedAt: ts("2025-12-31T12:00:00Z")},
		{ID: "other:p3", Harness: "other", NativeID: "p3", Cwd: "/w/tool", GitRoot: "/w/tool",
			StartedAt: ts("2026-10-15T10:00:00Z"), EndedAt: ts("2026-10-15T10:05:00Z")},
	}
	evs := []model.Event{
		// p1: one event before and one after midnight in Berlin (2026-10-31 is a Saturday).
		ev("claude:p1-1", "claude:p1", "2026-10-31T22:00:00Z", model.ActorAgent, model.KindMessage, "assistant", "a"),
		ev("claude:p1-2", "claude:p1", "2026-10-31T23:30:00Z", model.ActorHuman, model.KindMessage, "user", "b"),
		ev("claude:p1-3", "claude:p1", "2026-10-31T23:31:00Z", model.ActorTool, model.KindToolResult, "tool", "ignored"),
		// p1sub: two agent events in the parent's project.
		ev("claude:p1s-1", "claude:p1sub", "2026-10-31T22:05:00Z", model.ActorAgent, model.KindMessage, "assistant", "c"),
		ev("claude:p1s-2", "claude:p1sub", "2026-10-31T22:06:00Z", model.ActorAgent, model.KindToolUse, "assistant", "d"),
		// p2: Sunday 2025-12-28 (ISO 2025-W52) and Wednesday 2025-12-31 (ISO 2026-W01).
		ev("claude:p2-1", "claude:p2", "2025-12-28T12:00:00Z", model.ActorHuman, model.KindMessage, "user", "e"),
		ev("claude:p2-2", "claude:p2", "2025-12-31T12:00:00Z", model.ActorHuman, model.KindMessage, "user", "f"),
		// p3: the largest volume.
		ev("other:p3-1", "other:p3", "2026-10-15T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "g"),
		ev("other:p3-2", "other:p3", "2026-10-15T10:01:00Z", model.ActorAgent, model.KindMessage, "assistant", "h"),
		ev("other:p3-3", "other:p3", "2026-10-15T10:02:00Z", model.ActorAgent, model.KindMessage, "assistant", "i"),
		ev("other:p3-4", "other:p3", "2026-10-15T10:03:00Z", model.ActorAgent, model.KindMessage, "assistant", "j"),
		ev("other:p3-5", "other:p3", "2026-10-15T10:04:00Z", model.ActorAgent, model.KindMessage, "assistant", "k"),
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/a.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
}

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// cells returns the non-zero cells of a bucket as "key:sessions/human/agent".
func cells(b ActivityBucket) []string {
	var out []string
	for _, c := range b.Cells {
		if c.Activity == (Activity{}) {
			continue
		}
		out = append(out, fmt.Sprintf("%s:%d/%d/%d", c.Key, c.Sessions, c.Human, c.Agent))
	}
	return out
}

func TestActivitySeries(t *testing.T) {
	s, _ := openTestStore(t)
	seedActivity(t, s)
	loc := berlin(t)
	tests := []struct {
		name    string
		f       ActivityFilter
		keys    []string
		dirs    []string            // keys without a repository
		labels  []string            // all bucket labels in order
		cells   map[string][]string // label -> non-zero cells
		wantErr bool
	}{
		{
			name: "day boundary in Berlin, project, subagent inherits parent key",
			f: ActivityFilter{Since: ts("2026-10-30T23:00:00Z"), Until: ts("2026-11-01T23:00:00Z"),
				Location: loc, Period: PeriodDay, Dimension: DimProject},
			keys:   []string{"/w/app"},
			labels: []string{"2026-10-31", "2026-11-01"},
			cells: map[string][]string{
				"2026-10-31": {"/w/app:2/0/3"},
				"2026-11-01": {"/w/app:1/1/0"},
			},
		},
		{
			name: "day in UTC",
			f: ActivityFilter{Since: ts("2026-10-31T00:00:00Z"), Until: ts("2026-11-01T00:00:00Z"),
				Location: time.UTC, Period: PeriodDay, Dimension: DimProject},
			keys:   []string{"/w/app"},
			labels: []string{"2026-10-31"},
			cells:  map[string][]string{"2026-10-31": {"/w/app:2/1/3"}},
		},
		{
			name: "empty periods are filled, Since inside a period",
			f: ActivityFilter{Since: ts("2026-10-28T12:00:00Z"), Until: ts("2026-10-31T23:00:00Z"),
				Location: loc, Period: PeriodDay, Dimension: DimDirectory},
			keys:   []string{"/w/app"},
			labels: []string{"2026-10-28", "2026-10-29", "2026-10-30", "2026-10-31"},
			cells:  map[string][]string{"2026-10-31": {"/w/app:2/0/3"}},
		},
		{
			name: "ISO week across the year boundary",
			f: ActivityFilter{Since: ts("2025-12-22T00:00:00Z"), Until: ts("2026-01-04T23:00:00Z"),
				Location: loc, Period: PeriodWeek, Dimension: DimProject},
			keys:   []string{"/w/docs"},
			dirs:   []string{"/w/docs"},
			labels: []string{"2025-W52", "2026-W01"},
			cells: map[string][]string{
				"2025-W52": {"/w/docs:1/1/0"},
				"2026-W01": {"/w/docs:1/1/0"},
			},
		},
		{
			name: "months, keys by total volume, agent dimension",
			f: ActivityFilter{Since: ts("2025-12-01T00:00:00Z"), Until: ts("2026-11-30T23:00:00Z"),
				Location: loc, Period: PeriodMonth, Dimension: DimAgent},
			keys: []string{"other main", "claude main", "claude subagent"},
			labels: []string{"2025-12", "2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06",
				"2026-07", "2026-08", "2026-09", "2026-10", "2026-11"},
			cells: map[string][]string{
				"2025-12": {"claude main:1/2/0"},
				"2026-10": {"other main:1/1/4", "claude main:1/0/1", "claude subagent:1/0/2"},
				"2026-11": {"claude main:1/1/0"},
			},
		},
		{
			name: "TopN folds the rest into other, always last",
			f: ActivityFilter{Since: ts("2025-12-01T00:00:00Z"), Until: ts("2026-11-30T23:00:00Z"),
				Location: loc, Period: PeriodMonth, Dimension: DimProject, TopN: 2},
			keys: []string{"/w/tool", "/w/app", OtherKey},
			labels: []string{"2025-12", "2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06",
				"2026-07", "2026-08", "2026-09", "2026-10", "2026-11"},
			cells: map[string][]string{
				"2025-12": {"other:1/2/0"},
				"2026-10": {"/w/tool:1/1/4", "/w/app:2/0/3"},
				"2026-11": {"/w/app:1/1/0"},
			},
		},
		{
			name: "TopN larger than the number of keys adds no other",
			f: ActivityFilter{Since: ts("2026-10-01T00:00:00Z"), Until: ts("2026-10-31T23:00:00Z"),
				Location: loc, Period: PeriodMonth, Dimension: DimProject, TopN: 5},
			keys:   []string{"/w/tool", "/w/app"},
			labels: []string{"2026-10"},
			cells:  map[string][]string{"2026-10": {"/w/tool:1/1/4", "/w/app:2/0/3"}},
		},
		{
			name: "directory dimension, subagent counts under the parent cwd",
			f: ActivityFilter{Since: ts("2026-10-31T00:00:00Z"), Until: ts("2026-11-01T00:00:00Z"),
				Location: time.UTC, Period: PeriodDay, Dimension: DimDirectory},
			keys:   []string{"/w/app"},
			labels: []string{"2026-10-31"},
			cells:  map[string][]string{"2026-10-31": {"/w/app:2/1/3"}},
		},
		{
			name: "range without events yields empty buckets and no keys",
			f: ActivityFilter{Since: ts("2027-01-01T00:00:00Z"), Until: ts("2027-01-04T00:00:00Z"),
				Location: time.UTC, Period: PeriodDay, Dimension: DimProject},
			labels: []string{"2027-01-01", "2027-01-02", "2027-01-03"},
			cells:  map[string][]string{},
		},
		{
			name: "Since equal to Until yields no buckets",
			f: ActivityFilter{Since: ts("2026-10-15T00:00:00Z"), Until: ts("2026-10-15T00:00:00Z"),
				Location: time.UTC, Period: PeriodDay, Dimension: DimProject},
			cells: map[string][]string{},
		},
		{
			name:    "invalid period",
			f:       ActivityFilter{Since: ts("2026-10-01T00:00:00Z"), Until: ts("2026-11-01T00:00:00Z"), Period: Period(9)},
			wantErr: true,
		},
		{
			name:    "invalid dimension",
			f:       ActivityFilter{Since: ts("2026-10-01T00:00:00Z"), Until: ts("2026-11-01T00:00:00Z"), Dimension: Dimension(9)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ActivitySeries(ctx, tt.f)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got.Keys, ",") != strings.Join(tt.keys, ",") {
				t.Errorf("keys = %q, want %q", got.Keys, tt.keys)
			}
			if strings.Join(got.Dirs, ",") != strings.Join(tt.dirs, ",") {
				t.Errorf("dirs = %q, want %q", got.Dirs, tt.dirs)
			}
			var labels []string
			for _, b := range got.Buckets {
				labels = append(labels, b.Label)
				if len(b.Cells) != len(got.Keys) {
					t.Errorf("bucket %s has %d cells, want %d", b.Label, len(b.Cells), len(got.Keys))
				}
				for i, c := range b.Cells {
					if i < len(got.Keys) && c.Key != got.Keys[i] {
						t.Errorf("bucket %s cell %d key = %q, want %q", b.Label, i, c.Key, got.Keys[i])
					}
				}
				if want := tt.cells[b.Label]; strings.Join(cells(b), " ") != strings.Join(want, " ") {
					t.Errorf("bucket %s cells = %v, want %v", b.Label, cells(b), want)
				}
			}
			if strings.Join(labels, ",") != strings.Join(tt.labels, ",") {
				t.Errorf("labels = %q, want %q", labels, tt.labels)
			}
		})
	}
}

func TestActivitySeriesBucketStart(t *testing.T) {
	s, _ := openTestStore(t)
	seedActivity(t, s)
	loc := berlin(t)
	tests := []struct {
		name   string
		period Period
		since  string // RFC3339 in Berlin
		starts []string
	}{
		{"day", PeriodDay, "2026-10-31T00:00:00+01:00", []string{"2026-10-31T00:00:00+01:00", "2026-11-01T00:00:00+01:00"}},
		{"week starts Monday", PeriodWeek, "2026-10-28T00:00:00+01:00", []string{"2026-10-26T00:00:00+01:00"}},
		{"month", PeriodMonth, "2026-10-31T00:00:00+01:00", []string{"2026-10-01T00:00:00+02:00", "2026-11-01T00:00:00+01:00"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ActivitySeries(ctx, ActivityFilter{Since: ts(tt.since), Until: ts("2026-11-01T01:00:00+01:00"),
				Location: loc, Period: tt.period, Dimension: DimProject})
			if err != nil {
				t.Fatal(err)
			}
			var starts []string
			for _, b := range got.Buckets {
				if b.Start.Location() != loc {
					t.Errorf("start %v not in %v", b.Start, loc)
				}
				starts = append(starts, b.Start.Format(time.RFC3339))
			}
			if strings.Join(starts, ",") != strings.Join(tt.starts, ",") {
				t.Errorf("starts = %q, want %q", starts, tt.starts)
			}
		})
	}
}

func TestActivitySeriesOpenRange(t *testing.T) {
	s, _ := openTestStore(t)
	seedActivity(t, s)
	loc := berlin(t)
	// Without Since/Until the range spans the first to the last event.
	got, err := s.ActivitySeries(ctx, ActivityFilter{Location: loc, Period: PeriodDay, Dimension: DimProject})
	if err != nil {
		t.Fatal(err)
	}
	// 2025-12-28 .. 2026-11-01 inclusive: 4 + 304 + 1 days.
	if n := len(got.Buckets); n != 309 {
		t.Errorf("buckets = %d, want 309", n)
	}
	if got.Buckets[0].Label != "2025-12-28" || got.Buckets[len(got.Buckets)-1].Label != "2026-11-01" {
		t.Errorf("first/last = %s/%s", got.Buckets[0].Label, got.Buckets[len(got.Buckets)-1].Label)
	}
	if strings.Join(got.Keys, ",") != "/w/tool,/w/app,/w/docs" {
		t.Errorf("keys = %q", got.Keys)
	}
	// Open Since with Until set: from the first event up to Until.
	got, err = s.ActivitySeries(ctx, ActivityFilter{Until: ts("2026-01-01T00:00:00Z"), Location: loc, Period: PeriodWeek, Dimension: DimProject})
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, b := range got.Buckets {
		labels = append(labels, b.Label)
	}
	if strings.Join(labels, ",") != "2025-W52,2026-W01" {
		t.Errorf("labels = %q", labels)
	}
	// Empty store, open range: nothing.
	empty, _ := openTestStore(t)
	if got, err := empty.ActivitySeries(ctx, ActivityFilter{Period: PeriodDay}); err != nil || len(got.Buckets) != 0 || len(got.Keys) != 0 {
		t.Errorf("empty store: %+v, %v", got, err)
	}
}

func TestPeriodDimensionString(t *testing.T) {
	tests := []struct {
		got, want string
	}{
		{PeriodDay.String(), "day"}, {PeriodWeek.String(), "week"}, {PeriodMonth.String(), "month"}, {Period(9).String(), "Period(9)"},
		{DimProject.String(), "project"}, {DimAgent.String(), "agent"}, {DimDirectory.String(), "directory"}, {Dimension(9).String(), "Dimension(9)"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}

func TestParsePeriodDimension(t *testing.T) {
	periods := []struct {
		in      string
		want    Period
		wantErr bool
	}{
		{"day", PeriodDay, false},
		{"week", PeriodWeek, false},
		{"month", PeriodMonth, false},
		{"", 0, true},
		{"Day", 0, true},
		{"year", 0, true},
	}
	for _, tt := range periods {
		got, err := ParsePeriod(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParsePeriod(%q) = %v, %v; want %v, err %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
	dims := []struct {
		in      string
		want    Dimension
		wantErr bool
	}{
		{"project", DimProject, false},
		{"agent", DimAgent, false},
		{"directory", DimDirectory, false},
		{"", 0, true},
		{"repo", 0, true},
	}
	for _, tt := range dims {
		got, err := ParseDimension(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseDimension(%q) = %v, %v; want %v, err %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

// TestActivitySeriesDirs checks that a project key is reported in Dirs only
// when no session has it as git root: a directory that later became a
// repository counts as a repository.
func TestActivitySeriesDirs(t *testing.T) {
	s, _ := openTestStore(t)
	sessions := []model.Session{
		{ID: "claude:d1", Harness: "claude", NativeID: "d1", Cwd: "/w/notes",
			StartedAt: ts("2026-03-01T10:00:00Z"), EndedAt: ts("2026-03-01T10:05:00Z")},
		{ID: "claude:d2", Harness: "claude", NativeID: "d2", Cwd: "/w/later",
			StartedAt: ts("2026-03-01T11:00:00Z"), EndedAt: ts("2026-03-01T11:05:00Z")},
		{ID: "claude:d3", Harness: "claude", NativeID: "d3", Cwd: "/w/later/sub", GitRoot: "/w/later",
			StartedAt: ts("2026-03-02T11:00:00Z"), EndedAt: ts("2026-03-02T11:05:00Z")},
	}
	evs := []model.Event{
		ev("claude:d1-1", "claude:d1", "2026-03-01T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "a"),
		ev("claude:d2-1", "claude:d2", "2026-03-01T11:00:00Z", model.ActorHuman, model.KindMessage, "user", "b"),
		ev("claude:d3-1", "claude:d3", "2026-03-02T11:00:00Z", model.ActorHuman, model.KindMessage, "user", "c"),
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/d.jsonl", Harness: "claude", Offset: 1}, sessions, evs); err != nil {
		t.Fatal(err)
	}
	for _, d := range []Dimension{DimProject, DimDirectory, DimAgent} {
		got, err := s.ActivitySeries(ctx, ActivityFilter{Since: ts("2026-03-01T00:00:00Z"), Until: ts("2026-03-03T00:00:00Z"),
			Location: time.UTC, Period: PeriodDay, Dimension: d})
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		if d == DimProject {
			want = []string{"/w/notes"}
		}
		if strings.Join(got.Dirs, ",") != strings.Join(want, ",") {
			t.Errorf("%s: dirs = %q, want %q", d, got.Dirs, want)
		}
	}
}
