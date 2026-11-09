package viewer

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/linediff"
)

// diffContext is the number of unchanged lines around a change.
const diffContext = 3

// Diff shows the line diff of two versions of a memory file: path and
// project in the head, the two times with the number of added and removed
// lines, then the hunks with the added lines green and the removed ones
// red, each behind its + or -.
type Diff struct {
	// Path is the file, Project and Harness name its project.
	Path, Project, Harness string
	// From and To are the times of the old and the new version; FromNote
	// and ToNote are shown behind them ("version 2 of 3"). A zero From
	// means there is no old version: every line is new.
	From, To         time.Time
	FromNote, ToNote string
	// Old and New are the texts of the versions.
	Old, New string
}

// Render implements Viewer. There is no anchor: the diff starts at its top.
func (v Diff) Render(env Env, w int) Content {
	ops := linediff.Diff(linediff.SplitLines(v.Old), linediff.SplitLines(v.New))
	added, removed := linediff.Count(ops)
	th := env.Theme
	second := v.Project
	if v.Harness != "" {
		second += "  " + v.Harness
	}
	when := func(t time.Time, note string) string {
		return strings.TrimSpace(t.In(env.loc()).Format("2006-01-02 15:04") + " " + note)
	}
	from := "(no earlier version)"
	if !v.From.IsZero() {
		from = when(v.From, v.FromNote)
	}
	c := Content{Anchor: -1, Head: []string{
		" " + th.Title.Render(cut(v.Path, w)),
		" " + th.Muted.Render(cut(second, w)),
		" " + cut(from+" → "+when(v.To, v.ToNote), w),
		" " + th.Added.Render(fmt.Sprintf("+%d", added)) + " " + th.Removed.Render(fmt.Sprintf("-%d", removed)),
		rule(env, w),
	}}
	if added == 0 && removed == 0 {
		c.Body = []string{" " + th.Muted.Render("no differences")}
		return c
	}
	for _, h := range linediff.Hunks(ops, diffContext) {
		c.Body = append(c.Body, " "+th.Muted.Render(fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldLines, h.NewStart, h.NewLines)))
		for _, o := range h.Ops {
			c.Body = append(c.Body, diffLine(env, o, w)...)
		}
	}
	return c
}

// diffLine renders one line of a hunk, wrapped to w with the continuation
// lines indented behind the prefix.
func diffLine(env Env, o linediff.Op, w int) []string {
	text := strings.ReplaceAll(o.Text, "\t", "    ")
	room := max(w-3, 1)
	var parts []string
	if text == "" {
		parts = []string{""}
	} else {
		parts = strings.Split(ansi.Wrap(text, room, ""), "\n")
	}
	out := make([]string, len(parts))
	for i, p := range parts {
		prefix := " "
		if i == 0 {
			prefix = string(o.Kind)
		}
		line := " " + prefix + " " + p
		switch o.Kind {
		case linediff.Insert:
			line = env.Theme.Added.Render(line)
		case linediff.Delete:
			line = env.Theme.Removed.Render(line)
		}
		out[i] = line
	}
	return out
}
