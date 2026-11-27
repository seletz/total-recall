package search

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/demo"
)

// filterX is the column of the filter label word on the input line.
func filterX(t *testing.T, m Model, word string) int {
	t.Helper()
	line := ansi.Strip(m.inputLine())
	x := strings.LastIndex(line, word)
	if x < 0 {
		t.Fatalf("input line %q lacks %q", line, word)
	}
	return ansi.StringWidth(line[:x])
}

// TestFilterClick: the filters on the input line are controls: a click on
// actor or since cycles it like a and t, a click on repo opens the repo
// filter like r; the input keeps its focus where it had it, and a click
// beside them still focuses the input.
func TestFilterClick(t *testing.T) {
	m := demoModel(t, demo.Query)
	m = drive(t, m, click(filterX(t, m, "actor:"), 0))
	if m.filters.actor != 1 {
		t.Errorf("click on actor: actor = %d, want 1", m.filters.actor)
	}
	m = drive(t, m, click(filterX(t, m, "since:"), 0))
	m = drive(t, m, click(filterX(t, m, "since:"), 0)) // the label moved with its width
	if m.filters.span != 2 {
		t.Errorf("two clicks on since: span = %d, want 2", m.filters.span)
	}
	m = drive(t, m, click(filterX(t, m, "repo:"), 0))
	if m.focus != focusRepo {
		t.Errorf("click on repo: focus = %v", m.focus)
	}
	m = demoModel(t, demo.Query)
	m = drive(t, m, click(2, 0))
	if m.focus != focusInput || m.filters.actor != 0 {
		t.Errorf("click on the input: focus = %v, actor = %d", m.focus, m.filters.actor)
	}
}

// TestFilterSegments: the label is the segments in order, raw and the
// attachment filter only while they are on.
func TestFilterSegments(t *testing.T) {
	f := filters{attach: 2, raw: true}
	var kinds []string
	for _, s := range f.segments() {
		kinds = append(kinds, s.kind)
	}
	if got := strings.Join(kinds, ","); got != "actor,since,repo,attach,raw" {
		t.Errorf("segments = %s", got)
	}
	if got := (filters{}).label(); got != "actor: all  since: all  repo: *" {
		t.Errorf("label = %q", got)
	}
}

// TestIgnoredFilterIsShown: a filter that a selected type does not apply
// is never dropped silently: the label names the types that ignore it
// (#373). Where every selected type applies it, the label is unchanged.
func TestIgnoredFilterIsShown(t *testing.T) {
	tests := []struct {
		name  string
		keys  []string
		want  string
		other string
	}{
		{"event only", []string{"tab", "a", "a"}, "actor: agent", "(not"},
		{"input", []string{"tab", "a", "a", "u"}, "actor: agent (not input)", ""},
		{"several", []string{"tab", "a", "a", "u", "g", "c"}, "actor: agent (not input, answer, tool_call)", ""},
		{"file ignores actor too", []string{"tab", "a", "f"}, "actor: human (not file)", ""},
		{"all actors", []string{"tab", "u"}, "actor: all", "(not"},
		{"raw on session", []string{"tab", "s", "ctrl+r"}, "raw (not session)", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drive(t, demoModel(t, demo.Query), keysOf(tt.keys...)...)
			line := ansi.Strip(m.inputLine())
			if !strings.Contains(line, tt.want) {
				t.Errorf("line %q lacks %q", line, tt.want)
			}
			if tt.other != "" && strings.Contains(line, tt.other) {
				t.Errorf("line %q has %q", line, tt.other)
			}
		})
	}
	// The click targets follow the longer label.
	m := drive(t, demoModel(t, demo.Query), keysOf("tab", "a", "u")...)
	if got := m.filterAt(filterX(t, m, "since:")); got != "since" {
		t.Errorf("click on since lands on %q", got)
	}
}
