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
	case store.TypeInput:
		return k.TypeInput
	case store.TypeAnswer:
		return k.TypeAnswer
	case store.TypeToolCall:
		return k.TypeToolCall
	case store.TypeToolResult:
		return k.TypeToolResult
	case store.TypeImage:
		return k.TypeImage
	case store.TypeDocument:
		return k.TypeDocument
	case store.TypeFile:
		return k.TypeFile
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

// ruleZones are the clickable elements of the rule row: the project label
// and the sign that drops the pin (empty without a pin).
type ruleZones struct {
	project, clear zone.Span
}

// typeRow renders the row of the object types: one switcher per
// searchable type, "[h input]" for a selected one (title style) and
// "h input" for one that is off, the key first so that the switcher
// shows how to work it from the keyboard. When the row is too narrow the
// keys go, then the row is cut. The zones come from the same pass as the
// text (same order as searchableTypes), so clicks never drift.
func (m Model) typeRow(width int) (string, []zone.Span) {
	types := searchableTypes()
	label := func(t store.HitType, keys bool) string {
		name := string(t)
		if k := m.keys.typeKey(t); keys && len(k.Keys()) > 0 {
			name = keyLabel(k) + " " + name
		}
		if m.hasType(t) {
			name = "[" + name + "]"
		}
		return name
	}
	keys := true
	need := 2
	for _, t := range types {
		need += lipgloss.Width(label(t, true)) + 1
	}
	if need > width {
		keys = false
	}
	var b strings.Builder
	var spans []zone.Span
	st := m.th.Rule
	b.WriteString(st.Render("─ "))
	x := 2
	for _, t := range types {
		name := label(t, keys)
		w := lipgloss.Width(name)
		if m.hasType(t) {
			b.WriteString(m.th.Title.Render(name))
		} else {
			b.WriteString(m.th.Muted.Render(name))
		}
		spans = append(spans, zone.Span{Start: x, End: x + w})
		b.WriteString(" ")
		x += w + 1
	}
	line := ansi.Truncate(b.String(), width, "")
	line += st.Render(strings.Repeat("─", max(width-lipgloss.Width(line), 0)))
	return line, zone.Clip(spans, width)
}

// keyLabel is the key shown for a type switcher: the first of its bindings.
func keyLabel(k key.Binding) string { return k.Help().Key }

// ruleRow renders the rule between the type row and the body for the two
// columns: a plain rule in the left one and the project pin in the right
// one ("project: all", or the name with a sign to drop it). The zones come
// from the same pass as the text, so clicks never drift.
func (m Model) ruleRow(left, right int) (string, ruleZones) {
	var z ruleZones
	st := m.th.Rule
	dash := func(n int) string { return st.Render(strings.Repeat("─", max(n, 0))) }
	leftPart := dash(left)

	var b strings.Builder
	b.WriteString(st.Render("─ "))
	x := left + 1 + 2
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
