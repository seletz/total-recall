package viewer

import (
	"regexp"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/tui/session"
)

// Memory shows a file of the project memory: path, project and time of
// the version in the head, the Markdown of the file readable below. It is
// the viewer of a memory hit.
type Memory struct {
	// Path is the file within the memory directory, Project the name of
	// its project.
	Path, Project string
	// Harness is the harness the memory belongs to.
	Harness string
	// Changed is the time of the shown version.
	Changed time.Time
	// Content is the text of the file.
	Content string
}

// Render implements Viewer. The anchor is the first line with a match of
// the search when it lies deep in the file (see topLines), so the hit is in
// sight; otherwise there is none and the file starts at its top.
func (v Memory) Render(env Env, w int) Content {
	second := v.Project
	if v.Harness != "" {
		second += "  " + v.Harness
	}
	c := Content{Head: []string{
		" " + env.Theme.Title.Render(cut(v.Path, w)),
		" " + env.Theme.Muted.Render(cut(second, w)),
		" " + cut(v.Changed.In(env.loc()).Format("2006-01-02 15:04"), w),
		rule(env, w),
	}}
	c.Body, c.Anchor = markdown(env, v.Content, w)
	return c
}

var (
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	bulletRe  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	numberRe  = regexp.MustCompile(`^(\s*\d+[.)])\s+(.*)$`)
	boldRe    = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	codeRe    = regexp.MustCompile("`([^`]+)`")
)

// topLines is how far into a file a first match may lie before the view
// scrolls to it; a match nearer the top leaves the file at its start, with
// its front matter.
const topLines = 20

// markdown renders Markdown readable in a terminal: the front matter and
// code blocks muted, headings in the title style without their hashes,
// bullets as dots, the lines of a paragraph joined and wrapped, emphasis
// and inline code without their markers. It returns the lines and the
// index of the first line with a match of env.Terms that lies deeper than
// topLines, -1 for none.
func markdown(env Env, text string, w int) (lines []string, first int) {
	th := env.Theme
	plain := lipgloss.NewStyle()
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r", ""), "\t", "    ")
	src := strings.Split(strings.TrimRight(text, "\n"), "\n")
	inFront, inCode := len(src) > 0 && src[0] == "---", false
	first = -1
	var plainLines []string // the lines without styles, to find the matches
	emit := func(s string, base lipgloss.Style) {
		wrapped := strings.Split(ansi.Wrap(s, max(w-1, 1), ""), "\n")
		plainLines = append(plainLines, wrapped...)
		for _, l := range highlight(env, wrapped, base) {
			lines = append(lines, " "+l)
		}
	}
	blank := func() {
		lines = append(lines, "")
		plainLines = append(plainLines, "")
	}
	var para []string // lines of the paragraph being read
	flush := func() {
		if len(para) > 0 {
			emit(inline(strings.Join(para, " ")), plain)
			para = nil
		}
	}
	for i, l := range src {
		switch {
		case inFront:
			emit(l, th.Muted)
			if i > 0 && l == "---" {
				inFront = false
				blank()
			}
			continue
		case strings.HasPrefix(strings.TrimSpace(l), "```"):
			flush()
			inCode = !inCode
			continue
		case inCode:
			emit("  "+l, th.Muted)
			continue
		case strings.TrimSpace(l) == "":
			flush()
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				blank()
			}
			continue
		}
		if m := headingRe.FindStringSubmatch(l); m != nil {
			flush()
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				blank()
			}
			emit(inline(m[2]), th.Title)
			continue
		}
		if m := bulletRe.FindStringSubmatch(l); m != nil {
			flush()
			emit(m[1]+"• "+inline(m[2]), plain)
			continue
		}
		if m := numberRe.FindStringSubmatch(l); m != nil {
			flush()
			emit(m[1]+" "+inline(m[2]), plain)
			continue
		}
		para = append(para, strings.TrimSpace(l))
	}
	flush()
	if ms := session.Matches(plainLines, env.Terms); len(ms) > 0 && ms[0].Line >= topLines {
		first = ms[0].Line
	}
	return lines, first
}

// inline drops the markers of bold text and inline code.
func inline(s string) string {
	return codeRe.ReplaceAllString(boldRe.ReplaceAllString(s, "$1"), "$1")
}
