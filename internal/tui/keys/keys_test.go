package keys

import (
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

// optionKeys need ⌥ on a German Mac keyboard; no action may depend on one.
var optionKeys = []string{"[", "]", "{", "}", "|", "\\", "~", "@"}

func TestDefaultsReachableOnGermanMac(t *testing.T) {
	for _, a := range Actions {
		reachable := false
		for _, k := range a.Keys {
			if !slices.Contains(optionKeys, k) {
				reachable = true
			}
		}
		if !reachable {
			t.Errorf("%s: every default key (%v) needs the option key on a German Mac", a.Name, a.Keys)
		}
	}
}

func TestDefaultsAreValid(t *testing.T) {
	if _, err := Resolve(nil); err != nil {
		t.Fatalf("Resolve(nil) = %v", err)
	}
	seen := map[string]bool{}
	for _, a := range Actions {
		if seen[a.Name] {
			t.Errorf("%s listed twice", a.Name)
		}
		seen[a.Name] = true
		if !strings.HasPrefix(a.Name, string(a.Context)+"_") {
			t.Errorf("%s: name must start with its context %q", a.Name, a.Context)
		}
		if a.Help == "" || len(a.Keys) == 0 {
			t.Errorf("%s: help and keys must be set", a.Name)
		}
	}
}

func TestParseKey(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr string
	}{
		{"a", "a", ""},
		{"G", "G", ""},
		{"?", "?", ""},
		{",", ",", ""},
		{"ü", "ü", ""},
		{"up", "up", ""},
		{"pgdown", "pgdown", ""},
		{"f5", "f5", ""},
		{"ctrl+r", "ctrl+r", ""},
		{"shift+tab", "shift+tab", ""},
		{"alt+x", "alt+x", ""},
		{"shift+ctrl+up", "ctrl+shift+up", ""},
		{"CTRL+R", "ctrl+r", ""},
		{"Space", "space", ""},
		{"", "", "empty"},
		{" ", "", `"space"`},
		{"ctrl+", "", "empty"},
		{"ctrl+c", "", "reserved"},
		{"foo", "", "unknown key"},
		{"ab", "", "unknown key"},
		{"shift+a", "", "upper-case"},
		{"super+a", "", "unknown modifier"},
		{"ctrl+ctrl+a", "", "twice"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseKey(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseKey(%q) = %q, %v; want error containing %q", tt.in, got, err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("ParseKey(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string][]string
		want      map[string][]string // actions to check
		wantErr   string
	}{
		{"nil keeps defaults", nil, map[string][]string{GlobalQuit: {"q"}, StatsEarlier: {"left", ",", "["}}, ""},
		{"only the overridden action changes", map[string][]string{GlobalQuit: {"ctrl+q", "Q"}},
			map[string][]string{GlobalQuit: {"ctrl+q", "Q"}, GlobalHelp: {"?"}}, ""},
		{"keys are normalised", map[string][]string{SessionTop: {"Shift+Ctrl+Home"}}, map[string][]string{SessionTop: {"ctrl+shift+home"}}, ""},
		{"unknown action", map[string][]string{"stats_zoom": {"z"}}, nil, `"stats_zoom": unknown action`},
		{"no keys", map[string][]string{StatsDays: {}}, nil, "stats_days: no keys"},
		{"bad key", map[string][]string{StatsDays: {"ctrl+"}}, nil, `stats_days: "ctrl+": empty`},
		{"reserved", map[string][]string{GlobalQuit: {"ctrl+c"}}, nil, "reserved"},
		{"conflict within a view", map[string][]string{StatsDays: {"e"}}, nil, `"e" is bound to both stats_days and stats_metric`},
		{"conflict with a global key", map[string][]string{SearchActor: {"q"}}, nil, `"q" is bound to both global_quit and search_actor`},
		{"global may not take a view key", map[string][]string{GlobalHelp: {"h"}}, nil, `"h" is bound to both global_help and stats_heatmap`},
		{"same key twice in one action", map[string][]string{StatsDays: {"x", "x"}}, nil, `stats_days: "x" listed twice`},
		{"different views may share a key", map[string][]string{StatsDays: {"c"}}, map[string][]string{StatsDays: {"c"}, ActivityCurrent: {"c"}}, ""},
		{"swap within a view", map[string][]string{StatsDays: {"w"}, StatsWeeks: {"d"}}, map[string][]string{StatsDays: {"w"}, StatsWeeks: {"d"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := Resolve(tt.overrides)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Resolve error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			for action, want := range tt.want {
				if got := m.Keys(action); !slices.Equal(got, want) {
					t.Errorf("%s = %v, want %v", action, got, want)
				}
			}
			if len(m) != len(Actions) {
				t.Errorf("Resolve returned %d actions, want %d", len(m), len(Actions))
			}
		})
	}
}

func TestResolveErrorIsStable(t *testing.T) {
	// Two conflicts: the error names the first action in catalogue order,
	// not a random one from map iteration.
	for range 5 {
		_, err := Resolve(map[string][]string{StatsWeeks: {"d"}, StatsMonths: {"d"}})
		if err == nil || !strings.Contains(err.Error(), `"d" is bound to both stats_days and stats_weeks`) {
			t.Fatalf("Resolve error = %v", err)
		}
	}
}

func TestNilMapFallsBackToDefaults(t *testing.T) {
	var m Map
	if got := m.Keys(GlobalQuit); !slices.Equal(got, []string{"q"}) {
		t.Errorf("nil Map Keys(global_quit) = %v", got)
	}
	if b := m.Binding(GlobalQuit); b.Help().Key != "q" || b.Help().Desc != "quit" {
		t.Errorf("nil Map Binding(global_quit) help = %+v", b.Help())
	}
}

func TestBinding(t *testing.T) {
	m, err := Resolve(map[string][]string{SearchUp: {"up", "w", "i"}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		action           string
		wantKeys         []string
		wantKey, wantDes string
	}{
		{SearchUp, []string{"up", "w", "i"}, "↑/w/i", "up"},
		{SearchDown, []string{"down", "j"}, "↓/j", "down"},
		{SearchRaw, []string{"ctrl+r"}, "^r", "raw fts"},
		{SessionPageDown, []string{"f", "pgdown", "space"}, "f/pgdn/space", "page down"},
		{StatsEarlier, []string{"left", ",", "["}, "←/,/[", "earlier"},
		{SearchFocus, []string{"tab", "shift+tab"}, "tab/⇧tab", "input ↔ list"},
	}
	for _, tt := range tests {
		b := m.Binding(tt.action)
		if !slices.Equal(b.Keys(), tt.wantKeys) {
			t.Errorf("%s keys = %v, want %v", tt.action, b.Keys(), tt.wantKeys)
		}
		if h := b.Help(); h.Key != tt.wantKey || h.Desc != tt.wantDes {
			t.Errorf("%s help = %q %q, want %q %q", tt.action, h.Key, h.Desc, tt.wantKey, tt.wantDes)
		}
	}
	if _, ok := recoverPanic(func() { m.Binding("nope") }); !ok {
		t.Error("Binding of an unknown action must panic: it is a programming error")
	}
}

func TestGroup(t *testing.T) {
	m, err := Resolve(map[string][]string{StatsDays: {"x", "y"}})
	if err != nil {
		t.Fatal(err)
	}
	g := m.Group("period", StatsDays, StatsWeeks, StatsMonths)
	if want := []string{"x", "y", "w", "m"}; !slices.Equal(g.Keys(), want) {
		t.Errorf("group keys = %v, want %v", g.Keys(), want)
	}
	if h := g.Help(); h.Key != "x/w/m" || h.Desc != "period" {
		t.Errorf("group help = %+v", h)
	}
	r := m.Group("range", StatsEarlier, StatsLater, StatsZoomOut, StatsZoomIn)
	if h := r.Help(); h.Key != "←/→/-/+" {
		t.Errorf("range help key = %q", h.Key)
	}
}

func TestFirst(t *testing.T) {
	var m Map
	if got := m.First(GlobalBack); got != "esc" {
		t.Errorf("First(global_back) = %q", got)
	}
}

func TestDisplay(t *testing.T) {
	tests := map[string]string{
		"up": "↑", "down": "↓", "left": "←", "right": "→", "pgdown": "pgdn", "pgup": "pgup",
		"ctrl+r": "^r", "ctrl+shift+up": "^⇧↑", "shift+tab": "⇧tab", "alt+x": "alt+x", "a": "a", "space": "space", "esc": "esc",
	}
	for in, want := range tests {
		if got := Display(in); got != want {
			t.Errorf("Display(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestByContext(t *testing.T) {
	ctx := ByContext()
	if len(ctx[Global]) == 0 || len(ctx[Search]) == 0 || len(ctx[Session]) == 0 || len(ctx[Stats]) == 0 || len(ctx[Activity]) == 0 {
		t.Errorf("ByContext() = %v", ctx)
	}
	n := 0
	for _, as := range ctx {
		n += len(as)
	}
	if n != len(Actions) {
		t.Errorf("ByContext lists %d actions, want %d", n, len(Actions))
	}
}

func recoverPanic(f func()) (v any, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			v, ok = r, true
		}
	}()
	f()
	return nil, false
}

// TestPress checks that a synthesised key press matches its binding: the
// name Bubble Tea prints for it is the key name again, for every default
// key of the catalogue and the modifiers of the configuration.
func TestPress(t *testing.T) {
	names := []string{"ctrl+r", "alt+x", "shift+tab", "ctrl+shift+up", "ctrl+c", "space", "pgdown", "f5", "G", "ü"}
	for _, a := range Actions {
		names = append(names, a.Keys...)
	}
	for _, name := range names {
		if got := Press(name).String(); got != name {
			t.Errorf("Press(%q).String() = %q", name, got)
		}
	}
	if k := Press("a"); k.Text != "a" {
		t.Errorf("Press(a).Text = %q, want the character, so a text input inserts it", k.Text)
	}
	if k := Press("ctrl+a"); k.Text != "" {
		t.Errorf("Press(ctrl+a).Text = %q, want empty", k.Text)
	}
}

// TestShown checks that the keys a binding names in its help text are
// found again: every key of a single action, the first key of every action
// of a group.
func TestShown(t *testing.T) {
	m := Defaults()
	tests := []struct {
		name string
		b    key.Binding
		want []string
	}{
		{"one key", m.Binding(GlobalBack), []string{"esc"}},
		{"several keys", m.Binding(SessionUp), []string{"up", "k"}},
		{"ctrl", m.Binding(SearchRaw), []string{"ctrl+r"}},
		{"group", m.Group("period", StatsDays, StatsWeeks, StatsMonths), []string{"d", "w", "m"}},
		{"group of multi-key actions", m.Group("scroll", SessionUp, SessionDown), []string{"up", "down"}},
		{"group with range", m.Group("range", StatsEarlier, StatsLater, StatsZoomOut, StatsZoomIn), []string{"left", "right", "-", "+"}},
		{"force quit", ForceQuit, []string{"ctrl+c"}},
		{"slash key", key.NewBinding(key.WithKeys("/", "x"), key.WithHelp("//x", "find")), []string{"/", "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Shown(tt.b); !slices.Equal(got, tt.want) {
				t.Errorf("Shown = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEqual(t *testing.T) {
	quit, err := Resolve(map[string][]string{GlobalQuit: {"X"}})
	if err != nil {
		t.Fatal(err)
	}
	quitAgain, _ := Resolve(map[string][]string{GlobalQuit: {"X"}})
	tests := []struct {
		name string
		a, b Map
		want bool
	}{
		{"nil is the defaults", nil, Defaults(), true},
		{"resolved defaults", Defaults(), mustResolve(t, nil), true},
		{"same override", quit, quitAgain, true},
		{"override differs from the defaults", quit, nil, false},
		{"different overrides", quit, mustResolve(t, map[string][]string{GlobalQuit: {"Q"}}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equal(tt.b); got != tt.want {
				t.Errorf("Equal = %v, want %v", got, tt.want)
			}
			if got := tt.b.Equal(tt.a); got != tt.want {
				t.Errorf("Equal (swapped) = %v, want %v", got, tt.want)
			}
		})
	}
}

func mustResolve(t *testing.T, overrides map[string][]string) Map {
	t.Helper()
	m, err := Resolve(overrides)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
