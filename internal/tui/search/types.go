package search

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/zone"
)

// The object types the view can search, in display order: the searchable
// types of the store. event is the default.
func searchableTypes() []store.HitType {
	var out []store.HitType
	for _, ti := range store.HitTypes() {
		if ti.Searchable {
			out = append(out, ti.Type)
		}
	}
	return out
}

// typeKey is the binding that toggles type t, the zero Binding for a type
// without a key.
func (k KeyMap) typeKey(t store.HitType) key.Binding {
	switch t {
	case store.TypeEvent:
		return k.TypeEvent
	case store.TypeSession:
		return k.TypeSession
	case store.TypeMemory:
		return k.TypeMemory
	}
	return key.Binding{}
}

// hasType reports whether type t is selected.
func (m Model) hasType(t store.HitType) bool { return slices.Contains(m.types, t) }

// toggleType selects or deselects t; the last selected type stays. The
// selection keeps the display order of the types.
func (m *Model) toggleType(t store.HitType) bool {
	if !slices.Contains(searchableTypes(), t) {
		return false
	}
	var next []store.HitType
	for _, u := range searchableTypes() {
		if u == t && !m.hasType(t) || u != t && m.hasType(u) {
			next = append(next, u)
		}
	}
	if len(next) == 0 {
		return false
	}
	m.types = next
	return true
}

// ruleZones are the clickable elements of the rule row: one span per
// searchable type (same order as searchableTypes), the project label and
// the sign that drops the pin (empty without a pin).
type ruleZones struct {
	types          []zone.Span
	project, clear zone.Span
}

// ruleRow renders the rule between the input and the body for the two
// columns: the type switcher in the left one ("[event] session memory",
// the selected types in brackets and the title style) and the project pin
// in the right one ("project: all", or the name with a sign to drop it).
// The zones come from the same pass as the text, so clicks never drift.
func (m Model) ruleRow(left, right int) (string, ruleZones) {
	var z ruleZones
	st := m.th.Rule
	dash := func(n int) string { return st.Render(strings.Repeat("─", max(n, 0))) }

	var b strings.Builder
	b.WriteString(st.Render("─ "))
	x := 2
	for _, t := range searchableTypes() {
		name := string(t)
		w := lipgloss.Width(name)
		if m.hasType(t) {
			name, w = "["+name+"]", w+2
			b.WriteString(m.th.Title.Render(name))
		} else {
			b.WriteString(m.th.Muted.Render(name))
		}
		z.types = append(z.types, zone.Span{Start: x, End: x + w})
		b.WriteString(" ")
		x += w + 1
	}
	z.types = zone.Clip(z.types, left)
	leftPart := ansi.Truncate(b.String(), left, "")
	leftPart += dash(left - lipgloss.Width(leftPart))

	b.Reset()
	b.WriteString(st.Render("─ "))
	x = left + 1 + 2
	label := "project: "
	b.WriteString(m.th.Muted.Render(label))
	x += len(label)
	if m.pin == nil {
		b.WriteString(m.th.Muted.Render("all"))
		z.project = zone.Span{Start: left + 3, End: x + 3}
	} else {
		name := m.pin.name
		b.WriteString(m.th.Title.Render(name))
		z.project = zone.Span{Start: left + 3, End: x + lipgloss.Width(name)}
		x += lipgloss.Width(name)
		b.WriteString(" ")
		b.WriteString(m.th.Mark.Render("✕"))
		z.clear = zone.Span{Start: x + 1, End: x + 2}
	}
	b.WriteString(" ")
	full := left + 1 + right
	z.project, z.clear = z.project.Clip(full), z.clear.Clip(full)
	rightPart := ansi.Truncate(b.String(), right, "")
	rightPart += dash(right - lipgloss.Width(rightPart))
	return leftPart + st.Render("┬") + rightPart, z
}
