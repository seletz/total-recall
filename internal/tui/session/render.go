package session

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/model"
)

// mark is appended to the separator of the marked event; it reads without
// colour, like the brackets of the active tab in the app header.
const mark = " ◄"

// minIDWidth is the narrowest the session ID is shown at in the header.
const minIDWidth = 12

// layout rebuilds the transcript lines for the current width: one separator
// per event like `show` (time and actor, tool events with the tool name),
// then the text wrapped at word boundaries, then a blank line. Matches are
// recomputed on the wrapped lines.
func (m *Model) layout() {
	m.lines, m.eventAt = nil, nil
	width := max(m.width, 1)
	for i, ev := range m.events {
		m.eventAt = append(m.eventAt, len(m.lines))
		m.lines = append(m.lines, "── "+m.ts(ev)+"  "+Label(ev))
		text := strings.ReplaceAll(strings.TrimRight(ev.Text, "\n"), "\t", "    ")
		text = strings.ReplaceAll(text, "\r", "")
		if text != "" {
			m.lines = append(m.lines, strings.Split(ansi.Wrap(text, width, ""), "\n")...)
		}
		if i < len(m.events)-1 {
			m.lines = append(m.lines, "")
		}
	}
	m.matches = Matches(m.lines, m.open.Terms)
	if m.hit >= len(m.matches) {
		m.hit = m.hitFrom(0)
	}
	m.render()
}

// ts formats the time of an event for its separator.
func (m Model) ts(ev model.Event) string { return ev.TS.In(m.loc).Format("2006-01-02 15:04:05") }

// render styles the lines and hands them to the viewport: separators with
// the time muted and the actor in its colour (see Separator), the marked
// event's separator with mark, the text of tool events muted, hits in the
// match style, the selected hit reversed. A separator that
// holds a match keeps the plain, muted rendering, so the match shows. The
// scroll offset is kept.
func (m *Model) render() {
	out := make([]string, len(m.lines))
	next := 0
	event := -1 // the event the line belongs to
	for i, line := range m.lines {
		if event+1 < len(m.eventAt) && m.eventAt[event+1] == i {
			event++
		}
		start := next
		for next < len(m.matches) && m.matches[next].Line == i {
			next++
		}
		st := LineStyles{Match: m.th.Match, Selected: m.th.SelectedMatch}
		switch {
		case event >= 0 && m.eventAt[event] == i && start == next:
			out[i] = Separator(m.th, m.ts(m.events[event]), m.events[event], max(m.width, 1))
			continue
		case event >= 0 && m.eventAt[event] == i:
			st.Base = m.th.Muted
		case event >= 0:
			st.Base = textStyle(m.th, m.events[event])
		}
		out[i] = HighlightLine(line, m.matches[start:next], m.hit-start, st)
	}
	if m.current >= 0 && m.current < len(m.eventAt) {
		out[m.eventAt[m.current]] += m.th.Mark.Render(mark)
	}
	offset := m.vp.YOffset()
	m.vp.SetContentLines(out)
	m.vp.SetYOffset(offset)
}

// View implements tea.Model: two header lines, a rule carrying the status,
// and the viewport.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	var body string
	switch {
	case m.err != nil:
		body = " " + m.th.Error.Render(m.err.Error())
	case !m.loaded && m.loading:
		body = m.th.Muted.Render(" loading…")
	case !m.loaded:
		body = m.th.Muted.Render(" no session")
	default:
		body = lipgloss.JoinVertical(lipgloss.Left, m.header(), m.rule(), m.vp.View())
	}
	return tea.NewView(lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(body))
}

// header is the name (title style) with the ID (muted) on the right, then
// repo, branch, time span and the counts in the actors' colours. In a
// narrow window the name wins: the ID is shortened or dropped.
func (m Model) header() string {
	s := m.session
	name := s.DisplayName()
	if name == "" {
		name = "(untitled)"
	}
	left := " " + m.th.Title.Render(ansi.Truncate(name, max(m.width-2, 1), "…"))
	first := left
	if room := m.width - lipgloss.Width(left) - 3; room >= minIDWidth {
		id := ansi.Truncate(s.ID, room, "…")
		first += strings.Repeat(" ", room-lipgloss.Width(id)+2) + m.th.Muted.Render(id) + " "
	}

	parts := []string{ShortRepo(s.Repo())}
	if s.GitBranch != "" {
		parts = append(parts, s.GitBranch)
	}
	parts = append(parts, TimeSpan(s.StartedAt, s.EndedAt, m.loc),
		m.th.Actor(model.ActorHuman).Render(fmt.Sprintf("%d human", s.HumanEvents))+"  "+
			m.th.Actor(model.ActorAgent).Render(fmt.Sprintf("%d agent", s.AgentEvents)))
	if s.ParentID != "" {
		parts = append(parts, "parent "+s.ParentID)
	}
	second := ansi.Truncate(" "+strings.Join(parts, "  "), m.width, "…")
	return first + "\n" + second
}

// rule is the line between header and transcript; it carries the status:
// the event at the top, the selected hit, the position in a transcript
// longer than the viewport (top, bot or the percentage scrolled), the
// tools switch and the copy notice.
func (m Model) rule() string {
	parts := []string{"no events"}
	if n := len(m.events); n > 0 {
		parts[0] = fmt.Sprintf("%d/%d events", m.eventOf(m.vp.YOffset())+1, n)
	}
	switch {
	case len(m.matches) > 0:
		parts = append(parts, fmt.Sprintf("hit %d/%d", m.hit+1, len(m.matches)))
	case len(m.open.Terms) > 0:
		parts = append(parts, "no hits")
	}
	if m.vp.TotalLineCount() > m.vp.Height() {
		parts = append(parts, m.position())
	}
	if m.all {
		parts = append(parts, "tools")
	}
	if m.notice != "" {
		parts = append(parts, m.notice)
	}
	status := "── " + strings.Join(parts, " · ") + " "
	status = ansi.Truncate(status, m.width, "")
	return m.th.Rule.Render(status + strings.Repeat("─", max(m.width-lipgloss.Width(status), 0)))
}

// position is where the viewport is in a transcript longer than itself:
// "top", "bot" or the percentage scrolled, like a pager.
func (m Model) position() string {
	switch {
	case m.vp.AtTop():
		return "top"
	case m.vp.AtBottom():
		return "bot"
	}
	return fmt.Sprintf("%d%%", int(m.vp.ScrollPercent()*100))
}

// TimeSpan formats start and end like "2026-10-31 22:10 – 23:02 (52m)"; the
// end date is repeated when it differs from the start date.
func TimeSpan(start, end time.Time, loc *time.Location) string {
	if start.IsZero() {
		return ""
	}
	s, e := start.In(loc), end.In(loc)
	out := s.Format("2006-01-02 15:04")
	if end.IsZero() {
		return out
	}
	if s.Year() == e.Year() && s.YearDay() == e.YearDay() {
		out += " – " + e.Format("15:04")
	} else {
		out += " – " + e.Format("2006-01-02 15:04")
	}
	return out + " (" + duration(e.Sub(s)) + ")"
}

// duration formats d compactly: "45s", "12m", "3h05m", "2d04h".
func duration(d time.Duration) string {
	switch {
	case d < 0:
		return ""
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%02dh", int(d.Hours())/24, int(d.Hours())%24)
}

// ShortRepo returns "owner/repo" for a remote URL, else the base name of a
// path.
func ShortRepo(repo string) string {
	if repo == "" {
		return ""
	}
	if u, err := url.Parse(repo); err == nil && u.Host != "" {
		if p := strings.Trim(u.Path, "/"); p != "" {
			return p
		}
		return u.Host
	}
	return filepath.Base(repo)
}
