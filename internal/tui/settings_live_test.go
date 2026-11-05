package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/ops"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/tui/configview"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// liveApp builds the app on the Activity fixture with the configuration
// cfg, started as the CLI would start it from that configuration: the
// view, the working time, the holidays, the counting mode and the keys
// all come from cfg, so an edit on the settings page is the only change.
func liveApp(t *testing.T, h *reloadHarness, cfg *config.Config, start, query string) tea.Model {
	t.Helper()
	work, err := ops.WorkHours(cfg)
	if err != nil {
		t.Fatal(err)
	}
	km, err := cfg.KeyMap()
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Start: start, Query: query, Config: cfg, Keys: km, Home: cfg.Home,
		DayStart: cfg.DayStartOffset(), Work: work, Holidays: ops.Holidays(cfg), HumanOnly: cfg.Work.HumanOnly(),
		ActivityProjects: cfg.TUI.ActivityProjects, Reload: cfg.TUI.Reload.Duration,
	}
	return start_(t, h.app(t, opts))
}

// start_ is start without keys.
func start_(t *testing.T, app App) tea.Model {
	t.Helper()
	return start(t, app)
}

// editSetting changes key on the settings page of the config view the way
// a user does: switch to the view, go to the key (the list is walked from
// the top; headers are skipped by the cursor), press enter and, for a text
// value, replace the old text with text; "" presses enter only (a bool
// toggles, an enum takes the next value). The model ends on the settings
// page with the notice shown.
func editSetting(t *testing.T, m tea.Model, cfg *config.Config, key, text string) tea.Model {
	t.Helper()
	m = tuitest.Send(t, m, tuitest.Key("4"))
	if !strings.Contains(frame(m), "[settings]") {
		m = tuitest.Send(t, m, tuitest.Key("tab"))
	}
	index := -1
	for i, e := range cfg.Entries() {
		if e.Key == key {
			index = i
		}
	}
	if index < 0 {
		t.Fatalf("no setting %q", key)
	}
	for range len(cfg.Entries()) {
		m = tuitest.Send(t, m, tuitest.Key("k"))
	}
	for range index {
		m = tuitest.Send(t, m, tuitest.Key("j"))
	}
	m = tuitest.Send(t, m, tuitest.Key("enter"))
	if text != "" {
		for range 60 {
			m = tuitest.Send(t, m, tuitest.Key("backspace"))
		}
		for _, r := range text {
			m = tuitest.Send(t, m, tuitest.Key(string(r)))
		}
		m = tuitest.Send(t, m, tuitest.Key("enter"))
	}
	requireContains(t, frame(m), key+" written to")
	return m
}

// TestSettingsLive: a value changed on the settings page applies to the
// views at once, without a restart, and the views keep their window,
// their mark and their input. Every case starts the app from the
// configuration, edits one key as a user does and looks at the view
// again.
func TestSettingsLive(t *testing.T) {
	// A Saturday, so this week is 2026-01-05 – 2026-01-11 with Epiphanias
	// on Tuesday (Baden-Württemberg).
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, testfixture.ActivityLocation)
	tests := []struct {
		name   string
		start  string
		before []string // keys pressed in the view before the edit
		key    string
		edits  []string // text per enter; "" presses enter only
		after  []string // keys pressed after the edit
		want   []string
		lacks  []string
	}{
		{"work.target redraws the activity", ViewActivity, []string{"k"}, "work.target", []string{"4h"}, []string{"3"},
			[]string{"[3 activity]", "2026-01-04 – 2026-01-10", "× 4h00m"}, []string{"× 8h00m"}},
		{"work.bundesland reloads the holidays", ViewActivity, []string{"k"}, "work.bundesland", []string{"", ""}, []string{"3"},
			[]string{"[3 activity]", "2026-01-04 – 2026-01-10"}, []string{"Epiphanias"}},
		{"work.count switches the counting", ViewActivity, []string{"k"}, "work.count", []string{""}, []string{"3"},
			[]string{"[3 activity]", "2026-01-04 – 2026-01-10", "all events"}, []string{"human only"}},
		{"day_start moves the axis", ViewActivity, []string{"k"}, "day_start", []string{"00:00"}, []string{"3"},
			[]string{"[3 activity]", "2026-01-04 – 2026-01-10", "00      04"}, []string{"04      08"}},
		{"tui.activity_projects colours the bars", ViewActivity, []string{"k"}, "tui.activity_projects", []string{""}, []string{"3"},
			[]string{"[3 activity]", "2026-01-04 – 2026-01-10", "[projects]", "projects:"}, []string{"[hours]"}},
		{"tui.activity_newest flips the rows", ViewActivity, []string{"k"}, "tui.activity_newest", []string{""}, []string{"3"},
			[]string{"[3 activity]", "2026-01-04 – 2026-01-10", "day ↑"}, []string{"day ↓"}},
		{"keys.global_stats rebinds the view key", ViewActivity, nil, "keys.global_stats", []string{"S"}, []string{"3", "2", "S"},
			[]string{"[2 stats]", "• S stats"}, []string{"• 2 stats"}},
		{"keys.activity_earlier shows in the footer", ViewActivity, nil, "keys.activity_earlier", []string{"<"}, []string{"3", "<"},
			[]string{"[3 activity]", "2025-12-29 – 2026-01-04", "</→ earlier/later"}, nil},
		{"work.target says so", ViewActivity, nil, "work.target", []string{"4h"}, nil,
			[]string{"work.target written to", "· applied at once"}, nil},
		{"tui.view takes effect at the next start", ViewActivity, nil, "tui.view", []string{""}, nil,
			[]string{"tui.view written to", "· takes effect at the next start"}, nil},
		{"chart.scale is for the commands", ViewActivity, nil, "chart.scale", []string{"2"}, nil,
			[]string{"chart.scale written to", "· used by the next command or action"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newReloadHarness(t)
			h.now = now
			cfg := loadConfig(t, "")
			m := liveApp(t, h, cfg, tt.start, "")
			for _, k := range tt.before {
				m = tuitest.Send(t, m, tuitest.Key(k))
			}
			for _, text := range tt.edits {
				m = editSetting(t, m, cfg, tt.key, text)
			}
			for _, k := range tt.after {
				m = tuitest.Send(t, m, tuitest.Key(k))
			}
			f := frame(m)
			requireContains(t, f, tt.want...)
			requireLacks(t, f, tt.lacks...)
		})
	}
}

// TestSettingsLiveKeepsSearch: a key binding changed on the settings page
// leaves the search view as it was: the query, the focus and the marked
// hit; the old key does nothing, the new one acts.
func TestSettingsLiveKeepsSearch(t *testing.T) {
	h := newReloadHarness(t)
	cfg := loadConfig(t, "")
	m := liveApp(t, h, cfg, ViewSearch, "release")
	m = tuitest.Send(t, m, tuitest.Key("tab"))
	m = tuitest.Send(t, m, tuitest.Key("j"))
	before := frame(m)
	marked := markedLine(before)
	if marked == "" {
		t.Fatalf("no marked hit:\n%s", before)
	}
	m = editSetting(t, m, cfg, "keys.search_actor", "A")
	m = tuitest.Send(t, m, tuitest.Key("1"))
	after := frame(m)
	requireContains(t, after, "> release", "A actor")
	if got := markedLine(after); got != marked {
		t.Errorf("marked hit moved from %q to %q:\n%s", marked, got, after)
	}
	m = tuitest.Send(t, m, tuitest.Key("a"))
	if f := frame(m); f != after {
		t.Errorf("a still switches the actor filter:\n%s", f)
	}
	m = tuitest.Send(t, m, tuitest.Key("A"))
	if f := frame(m); f == after {
		t.Errorf("A does not switch the actor filter:\n%s", f)
	}
}

// TestSettingsLiveProblem: a configuration with a problem in [work] or
// [keys] (reloaded after an external edit) keeps the values in effect.
func TestSettingsLiveProblem(t *testing.T) {
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, testfixture.ActivityLocation)
	tests := []struct {
		name string
		text string
	}{
		{"bad core hours", strings.Replace(config.Template, "hours = \"09:00-17:00\"", "hours = \"25:00-17:00\"", 1)},
		{"key conflict", config.Template + "\nstats_days = \"e\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newReloadHarness(t)
			h.now = now
			m := liveApp(t, h, loadConfig(t, ""), ViewActivity, "")
			before := frame(m)
			bad := loadConfigText(t, tt.text)
			if len(bad.Problems()) == 0 {
				t.Fatal("the configuration has no problem")
			}
			m = tuitest.Send(t, m, configview.Changed{Config: bad})
			if after := frame(m); after != before {
				t.Errorf("the view changed:\n%s", after)
			}
			m = tuitest.Send(t, m, tuitest.Key("d"))
			requireContains(t, frame(m), "[day]")
		})
	}
}
