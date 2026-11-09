// Package keys is the catalogue of every key-bound action of the TUI: its
// name, the view it belongs to, its default keys and its help text. The
// config package validates the [keys] table against it, the views build
// their key maps from it, so the footer and the help overlay always show
// the keys that are in effect.
//
// Defaults are chosen for a German Mac keyboard: no action depends on a
// key that needs the option key there ([ ] { } | \ ~ @); such keys only
// ever come second. ctrl+c quits everywhere and cannot be rebound.
package keys

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Context is the view an action belongs to. Keys of one context may not
// collide; global keys apply in every view and may not be reused by one.
type Context string

// Contexts, in the order of the help overlay and the docs.
const (
	Global   Context = "global"
	Search   Context = "search"
	Session  Context = "session"
	Memory   Context = "memory"
	Stats    Context = "stats"
	Activity Context = "activity"
	Config   Context = "config"
)

// Action is one key-bound action.
type Action struct {
	Name    string
	Context Context
	// Keys are the default bindings, the most convenient one first: it
	// is the one the footer names in a group.
	Keys []string
	// Help is the description in the footer and the help overlay.
	Help string
}

// Action names: the keys of the [keys] table, prefixed with their context.
const (
	GlobalSearch   = "global_search"
	GlobalStats    = "global_stats"
	GlobalActivity = "global_activity"
	GlobalHelp     = "global_help"
	GlobalBack     = "global_back"
	GlobalQuit     = "global_quit"
	GlobalReload   = "global_reload"
	GlobalConfig   = "global_config"

	SearchFocus = "search_focus"
	SearchOpen  = "search_open"
	SearchUp    = "search_up"
	SearchDown  = "search_down"
	SearchActor = "search_actor"
	SearchTime  = "search_time"
	SearchRepo  = "search_repo"
	SearchRaw   = "search_raw"

	SearchTypeEvent   = "search_type_event"
	SearchTypeSession = "search_type_session"
	SearchTypeMemory  = "search_type_memory"
	SearchPin         = "search_pin"
	SearchProject     = "search_project"

	SessionUp       = "session_up"
	SessionDown     = "session_down"
	SessionPageUp   = "session_page_up"
	SessionPageDown = "session_page_down"
	SessionHalfUp   = "session_half_up"
	SessionHalfDown = "session_half_down"
	SessionTop      = "session_top"
	SessionBottom   = "session_bottom"
	SessionNext     = "session_next"
	SessionPrev     = "session_prev"
	SessionTools    = "session_tools"
	SessionCopy     = "session_copy"

	MemoryUp       = "memory_up"
	MemoryDown     = "memory_down"
	MemoryOpen     = "memory_open"
	MemoryPageUp   = "memory_page_up"
	MemoryPageDown = "memory_page_down"
	MemoryDiff     = "memory_diff"
	MemoryBase     = "memory_base"
	MemoryTimeline = "memory_timeline"

	StatsDays      = "stats_days"
	StatsWeeks     = "stats_weeks"
	StatsMonths    = "stats_months"
	StatsProject   = "stats_project"
	StatsAgent     = "stats_agent"
	StatsDirectory = "stats_directory"
	StatsEarlier   = "stats_earlier"
	StatsLater     = "stats_later"
	StatsZoomOut   = "stats_zoom_out"
	StatsZoomIn    = "stats_zoom_in"
	StatsMetric    = "stats_metric"
	StatsHeatmap   = "stats_heatmap"
	StatsGraphics  = "stats_graphics"

	ActivityDay      = "activity_day"
	ActivityWeek     = "activity_week"
	ActivityMonth    = "activity_month"
	ActivityEarlier  = "activity_earlier"
	ActivityLater    = "activity_later"
	ActivityCurrent  = "activity_current"
	ActivityLast     = "activity_last"
	ActivityHuman    = "activity_human"
	ActivityFigures  = "activity_figures"
	ActivityProjects = "activity_projects"
	ActivityOrder    = "activity_order"
	ActivityUp       = "activity_up"
	ActivityDown     = "activity_down"

	ConfigNext    = "config_next"
	ConfigPrev    = "config_prev"
	ConfigUp      = "config_up"
	ConfigDown    = "config_down"
	ConfigSelect  = "config_select"
	ConfigDefault = "config_default"
	ConfigConfirm = "config_confirm"
	ConfigCopy    = "config_copy"
	ConfigEditor  = "config_editor"
	ConfigAdd     = "config_add"
	ConfigRemove  = "config_remove"
	ConfigMoveUp  = "config_move_up"
	ConfigMoveDn  = "config_move_down"
	ConfigPattern = "config_pattern"
)

// Actions is the catalogue, in the order of the docs and of `config show`.
var Actions = []Action{
	{GlobalSearch, Global, []string{"1"}, "search"},
	{GlobalStats, Global, []string{"2"}, "stats"},
	{GlobalActivity, Global, []string{"3"}, "activity"},
	{GlobalHelp, Global, []string{"?"}, "help"},
	{GlobalBack, Global, []string{"esc"}, "back"},
	{GlobalQuit, Global, []string{"q"}, "quit"},
	{GlobalReload, Global, []string{"R"}, "reload"},
	{GlobalConfig, Global, []string{"4"}, "config"},

	{SearchFocus, Search, []string{"tab", "shift+tab"}, "input ↔ list"},
	{SearchOpen, Search, []string{"enter"}, "open"},
	{SearchUp, Search, []string{"up", "k"}, "up"},
	{SearchDown, Search, []string{"down", "j"}, "down"},
	{SearchActor, Search, []string{"a"}, "actor"},
	{SearchTime, Search, []string{"t"}, "time"},
	{SearchRepo, Search, []string{"r"}, "repo"},
	{SearchRaw, Search, []string{"ctrl+r"}, "raw fts"},
	{SearchTypeEvent, Search, []string{"e"}, "events on/off"},
	{SearchTypeSession, Search, []string{"s"}, "sessions on/off"},
	{SearchTypeMemory, Search, []string{"m"}, "memory on/off"},
	{SearchPin, Search, []string{"p"}, "pin project of hit"},
	{SearchProject, Search, []string{"P"}, "choose project"},

	{SessionUp, Session, []string{"up", "k"}, "up"},
	{SessionDown, Session, []string{"down", "j"}, "down"},
	{SessionPageUp, Session, []string{"b", "pgup"}, "page up"},
	{SessionPageDown, Session, []string{"f", "pgdown", "space"}, "page down"},
	{SessionHalfUp, Session, []string{"u", "ctrl+u"}, "½ page up"},
	{SessionHalfDown, Session, []string{"d", "ctrl+d"}, "½ page down"},
	{SessionTop, Session, []string{"g", "home"}, "top"},
	{SessionBottom, Session, []string{"G", "end"}, "bottom"},
	{SessionNext, Session, []string{"n"}, "next hit"},
	{SessionPrev, Session, []string{"N"}, "previous hit"},
	{SessionTools, Session, []string{"T"}, "tools on/off"},
	{SessionCopy, Session, []string{"y"}, "copy session id"},

	{MemoryUp, Memory, []string{"up", "k"}, "up"},
	{MemoryDown, Memory, []string{"down", "j"}, "down"},
	{MemoryOpen, Memory, []string{"enter"}, "history of file"},
	{MemoryPageUp, Memory, []string{"b", "pgup"}, "page up"},
	{MemoryPageDown, Memory, []string{"f", "pgdown", "space"}, "page down"},
	{MemoryDiff, Memory, []string{"d"}, "diff on/off"},
	{MemoryBase, Memory, []string{"c"}, "compare with this version"},
	{MemoryTimeline, Memory, []string{"t"}, "timeline"},

	{StatsDays, Stats, []string{"d"}, "days"},
	{StatsWeeks, Stats, []string{"w"}, "weeks"},
	{StatsMonths, Stats, []string{"m"}, "months"},
	{StatsProject, Stats, []string{"p"}, "by project"},
	{StatsAgent, Stats, []string{"a"}, "by agent"},
	{StatsDirectory, Stats, []string{"v"}, "by directory"},
	{StatsEarlier, Stats, []string{"left", ",", "["}, "earlier"},
	{StatsLater, Stats, []string{"right", ".", "]"}, "later"},
	{StatsZoomOut, Stats, []string{"-"}, "zoom out"},
	{StatsZoomIn, Stats, []string{"+", "="}, "zoom in"},
	{StatsMetric, Stats, []string{"e"}, "metric"},
	{StatsHeatmap, Stats, []string{"h"}, "heatmap"},
	{StatsGraphics, Stats, []string{"g"}, "graphics"},

	{ActivityDay, Activity, []string{"d"}, "one day"},
	{ActivityWeek, Activity, []string{"w"}, "one week"},
	{ActivityMonth, Activity, []string{"m"}, "one month"},
	{ActivityEarlier, Activity, []string{"left", ",", "["}, "earlier"},
	{ActivityLater, Activity, []string{"right", ".", "]"}, "later"},
	{ActivityCurrent, Activity, []string{"c"}, "this day/week/month"},
	{ActivityLast, Activity, []string{"l"}, "last day/week/month"},
	{ActivityHuman, Activity, []string{"h"}, "human/all events"},
	{ActivityFigures, Activity, []string{"s"}, "summary figures"},
	{ActivityProjects, Activity, []string{"p"}, "by project"},
	{ActivityOrder, Activity, []string{"o"}, "newest top/bottom"},
	{ActivityUp, Activity, []string{"up", "k"}, "row above (scroll)"},
	{ActivityDown, Activity, []string{"down", "j"}, "row below (scroll)"},

	{ConfigNext, Config, []string{"tab", "right"}, "next page"},
	{ConfigPrev, Config, []string{"shift+tab", "left"}, "previous page"},
	{ConfigUp, Config, []string{"up", "k"}, "up"},
	{ConfigDown, Config, []string{"down", "j"}, "down"},
	{ConfigSelect, Config, []string{"enter"}, "edit / run"},
	{ConfigDefault, Config, []string{"d"}, "reset to default"},
	{ConfigConfirm, Config, []string{"y"}, "confirm"},
	{ConfigCopy, Config, []string{"c"}, "copy hook or MCP block"},
	{ConfigEditor, Config, []string{"E"}, "open config in $EDITOR"},
	{ConfigAdd, Config, []string{"a"}, "add"},
	{ConfigRemove, Config, []string{"x"}, "remove"},
	{ConfigMoveUp, Config, []string{"K"}, "move up"},
	{ConfigMoveDn, Config, []string{"J"}, "move down"},
	{ConfigPattern, Config, []string{"m"}, "add as pattern"},
}

// ForceQuit is the fixed binding that quits in every state, even while a
// text field has the focus; it is not part of the catalogue.
var ForceQuit = key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp(Display("ctrl+c"), "quit"))

// Contexts lists the contexts in catalogue order.
var Contexts = []Context{Global, Search, Session, Memory, Stats, Activity, Config}

// ByContext groups the catalogue by context, keeping the order.
func ByContext() map[Context][]Action {
	out := map[Context][]Action{}
	for _, a := range Actions {
		out[a.Context] = append(out[a.Context], a)
	}
	return out
}

// Lookup returns the catalogue entry of an action name.
func Lookup(name string) (Action, bool) {
	for _, a := range Actions {
		if a.Name == name {
			return a, true
		}
	}
	return Action{}, false
}

// Map holds the effective keys of every action. A nil Map yields the
// defaults, so views work without configuration.
type Map map[string][]string

// Defaults returns the catalogue's bindings.
func Defaults() Map {
	m := make(Map, len(Actions))
	for _, a := range Actions {
		m[a.Name] = slices.Clone(a.Keys)
	}
	return m
}

// Resolve applies overrides (the [keys] table: action → keys) to the
// defaults and validates the result: every action must exist, every key
// must parse, and within a view, as well as between a view and the global
// actions, a key may belong to one action only. Only overridden actions
// change. Keys are returned normalised (lower-case names, modifiers in the
// order ctrl, alt, shift).
func Resolve(overrides map[string][]string) (Map, error) {
	m := Defaults()
	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if _, ok := Lookup(name); !ok {
			return nil, fmt.Errorf("[keys] %q: unknown action (see the key reference in the TUI docs)", name)
		}
		raw := overrides[name]
		if len(raw) == 0 {
			return nil, fmt.Errorf("[keys] %s: no keys", name)
		}
		ks := make([]string, 0, len(raw))
		for _, r := range raw {
			k, err := ParseKey(r)
			if err != nil {
				return nil, fmt.Errorf("[keys] %s: %w", name, err)
			}
			if slices.Contains(ks, k) {
				return nil, fmt.Errorf("[keys] %s: %q listed twice", name, k)
			}
			ks = append(ks, k)
		}
		m[name] = ks
	}
	return m, m.conflicts()
}

// conflicts reports the first key that is bound twice within a context or
// between a view and the global actions, in catalogue order.
func (m Map) conflicts() error {
	global := map[string]string{} // key → global action
	for _, a := range Actions {
		if a.Context != Global {
			continue
		}
		for _, k := range m[a.Name] {
			if other, dup := global[k]; dup {
				return fmt.Errorf("[keys] %q is bound to both %s and %s", k, other, a.Name)
			}
			global[k] = a.Name
		}
	}
	for _, ctx := range Contexts[1:] {
		seen := map[string]string{}
		for k, name := range global {
			seen[k] = name
		}
		for _, a := range Actions {
			if a.Context != ctx {
				continue
			}
			for _, k := range m[a.Name] {
				if other, dup := seen[k]; dup {
					return fmt.Errorf("[keys] %q is bound to both %s and %s", k, other, a.Name)
				}
				seen[k] = a.Name
			}
		}
	}
	return nil
}

// Keys returns the effective keys of an action; the defaults for a nil
// Map or an action it does not hold.
func (m Map) Keys(action string) []string {
	if ks, ok := m[action]; ok {
		return ks
	}
	a, ok := Lookup(action)
	if !ok {
		panic("keys: unknown action " + action)
	}
	return a.Keys
}

// First returns the first key of an action, the one named in footer groups.
func (m Map) First(action string) string { return m.Keys(action)[0] }

// Binding returns the bubbles binding of an action: all its keys, the help
// text from the catalogue with the keys as the user sees them.
func (m Map) Binding(action string) key.Binding {
	a, ok := Lookup(action)
	if !ok {
		panic("keys: unknown action " + action)
	}
	ks := m.Keys(action)
	shown := make([]string, len(ks))
	for i, k := range ks {
		shown[i] = Display(k)
	}
	return key.NewBinding(key.WithKeys(ks...), key.WithHelp(strings.Join(shown, "/"), a.Help))
}

// Group returns one binding for several actions, for the footer: all their
// keys, shown as the first key of each action ("d/w/m").
func (m Map) Group(help string, actions ...string) key.Binding {
	var ks, shown []string
	for _, action := range actions {
		ks = append(ks, m.Keys(action)...)
		shown = append(shown, Display(m.First(action)))
	}
	return key.NewBinding(key.WithKeys(ks...), key.WithHelp(strings.Join(shown, "/"), help))
}

// modifiers in the order Bubble Tea prints them.
var modifiers = []string{"ctrl", "alt", "shift"}

// named are the key names Bubble Tea produces besides single characters.
var named = map[string]bool{
	"enter": true, "tab": true, "backspace": true, "esc": true, "space": true,
	"up": true, "down": true, "left": true, "right": true,
	"insert": true, "delete": true, "pgup": true, "pgdown": true, "home": true, "end": true,
}

// ParseKey validates a key name from the configuration and returns it as
// Bubble Tea names it: a single character ("a", "G", "?", ","), a named
// key (up, down, left, right, enter, tab, esc, space, backspace, delete,
// insert, home, end, pgup, pgdown, f1…f20), optionally with the modifiers
// ctrl+, alt+ and shift+ in front. Names are case-insensitive except for a
// single character on its own: shift+a is spelled "A", while ctrl+R and
// ctrl+r are the same key. ctrl+c is reserved.
func ParseKey(s string) (string, error) {
	if s == " " {
		return "", fmt.Errorf(`" ": the space bar is "space"`)
	}
	parts := strings.Split(s, "+")
	base := parts[len(parts)-1]
	mods := parts[:len(parts)-1]
	if base == "" {
		return "", fmt.Errorf("%q: empty key name", s)
	}
	var out strings.Builder
	have := map[string]bool{}
	for _, mod := range mods {
		mod = strings.ToLower(mod)
		if !slices.Contains(modifiers, mod) {
			return "", fmt.Errorf("%q: unknown modifier %q (use ctrl, alt or shift)", s, mod)
		}
		if have[mod] {
			return "", fmt.Errorf("%q: modifier %q twice", s, mod)
		}
		have[mod] = true
	}
	for _, mod := range modifiers {
		if have[mod] {
			out.WriteString(mod + "+")
		}
	}
	if utf8.RuneCountInString(base) == 1 {
		r, _ := utf8.DecodeRuneInString(base)
		if !unicode.IsPrint(r) {
			return "", fmt.Errorf("%q: unknown key", s)
		}
		if have["shift"] && !have["ctrl"] && !have["alt"] {
			return "", fmt.Errorf("%q: spell shift with a character as the upper-case character", s)
		}
		if len(have) > 0 {
			// With ctrl or alt the terminal reports the lower-case letter.
			base = strings.ToLower(base)
		}
		out.WriteString(base)
	} else {
		base = strings.ToLower(base)
		if !named[base] && !isFunctionKey(base) {
			return "", fmt.Errorf("%q: unknown key %q", s, base)
		}
		out.WriteString(base)
	}
	k := out.String()
	if k == "ctrl+c" {
		return "", fmt.Errorf("%q: reserved, ctrl+c always quits", s)
	}
	return k, nil
}

// isFunctionKey reports f1…f20.
func isFunctionKey(s string) bool {
	n, ok := strings.CutPrefix(s, "f")
	if !ok || n == "" || len(n) > 2 {
		return false
	}
	v := 0
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
		v = v*10 + int(r-'0')
	}
	return v >= 1 && v <= 20
}

// display names of keys in the footer and the help overlay.
var display = map[string]string{
	"up": "↑", "down": "↓", "left": "←", "right": "→", "pgdown": "pgdn",
}

// Display returns a key name as the footer shows it: arrows as glyphs,
// ctrl+ as ^, shift+ as ⇧, pgdown as pgdn.
func Display(k string) string {
	var out strings.Builder
	rest := k
	for {
		if r, ok := strings.CutPrefix(rest, "ctrl+"); ok {
			out.WriteString("^")
			rest = r
		} else if r, ok := strings.CutPrefix(rest, "shift+"); ok {
			out.WriteString("⇧")
			rest = r
		} else if r, ok := strings.CutPrefix(rest, "alt+"); ok {
			out.WriteString("alt+")
			rest = r
		} else {
			break
		}
	}
	if d, ok := display[rest]; ok {
		rest = d
	}
	out.WriteString(rest)
	return out.String()
}

// codes are the rune codes of the named keys ParseKey accepts.
var codes = map[string]rune{
	"enter": tea.KeyEnter, "tab": tea.KeyTab, "backspace": tea.KeyBackspace, "esc": tea.KeyEscape, "space": tea.KeySpace,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"insert": tea.KeyInsert, "delete": tea.KeyDelete, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
}

// Press returns the key press Bubble Tea would deliver for a key name as
// ParseKey returns it, so a click on a key hint can be handled exactly like
// the key: key.Matches recognises it, and a text input inserts a plain
// character. An unknown name yields a press that matches nothing.
func Press(name string) tea.KeyPressMsg {
	var k tea.KeyPressMsg
	rest := name
	for {
		if r, ok := strings.CutPrefix(rest, "ctrl+"); ok {
			k.Mod, rest = k.Mod|tea.ModCtrl, r
		} else if r, ok := strings.CutPrefix(rest, "alt+"); ok {
			k.Mod, rest = k.Mod|tea.ModAlt, r
		} else if r, ok := strings.CutPrefix(rest, "shift+"); ok {
			k.Mod, rest = k.Mod|tea.ModShift, r
		} else {
			break
		}
	}
	switch {
	case codes[rest] != 0:
		k.Code = codes[rest]
		if rest == "space" && k.Mod == 0 {
			k.Text = " "
		}
	case isFunctionKey(rest):
		n := 0
		for _, r := range rest[1:] {
			n = n*10 + int(r-'0')
		}
		k.Code = tea.KeyF1 + rune(n-1)
	case utf8.RuneCountInString(rest) == 1:
		k.Code, _ = utf8.DecodeRuneInString(rest)
		if k.Mod == 0 {
			k.Text = rest
		}
	}
	return k
}

// Shown returns the keys a binding names in its help text, in that order:
// every key of a binding of one action, the first key of every action of a
// group. The help text lists the display names (see Display) joined by
// slashes; a key the text does not name is left out.
func Shown(b key.Binding) []string {
	var out []string
	rest := b.Help().Key
	for _, k := range b.Keys() {
		d := Display(k)
		after, ok := strings.CutPrefix(rest, d)
		if !ok || (after != "" && !strings.HasPrefix(after, "/")) {
			continue
		}
		out = append(out, k)
		rest = strings.TrimPrefix(after, "/")
	}
	return out
}

// Equal reports whether m and o bind the same keys to every action; a nil
// Map counts as the defaults.
func (m Map) Equal(o Map) bool {
	for _, a := range Actions {
		if !slices.Equal(m.Keys(a.Name), o.Keys(a.Name)) {
			return false
		}
	}
	return true
}
