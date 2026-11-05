package activity

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// TestFillsHeight checks the body height is used (#173): at every height,
// for every window, with and without the figures and the legend, at both
// scroll edges and after a resize, the frame has exactly the body's lines,
// as many day rows as fit (all of them when they do), and no empty line
// while a day row is hidden.
func TestFillsHeight(t *testing.T) {
	windows := []struct {
		name string
		keys []string
	}{
		{"Day", []string{"d"}},
		{"Week", nil},
		{"WeekSliding", repeat("k", 3)},
		{"WeekProjects", []string{"p"}},
		{"WeekProjectsNoFigures", []string{"p", "s"}},
		{"Month", []string{"m", "l"}},
		{"MonthEnd", append([]string{"m", "l"}, repeat("j", 40)...)},
		{"MonthNoFigures", []string{"m", "l", "s"}},
		{"MonthProjectsEnd", append([]string{"m", "l", "p"}, repeat("j", 40)...)},
		{"MonthProjectsNoFiguresEnd", append([]string{"m", "l", "p", "s"}, repeat("j", 40)...)},
		{"MonthFiguresBackOn", []string{"m", "l", "s", "s"}},
		{"MonthProjectsOff", []string{"m", "l", "p", "p"}},
	}
	// Heights of the terminal; the body is four lines shorter. The last
	// one is the resize target after the others.
	heights := []int{24, 40, 60}
	st, _ := fixturedb.Activity(t)
	for _, w := range windows {
		for _, h := range heights {
			t.Run(fmt.Sprintf("%s/%d", w.name, h), func(t *testing.T) {
				size := tea.WindowSizeMsg{Width: 120, Height: h - 4}
				tm := tuitest.Drive(t, newModel(t, st), append([]tea.Msg{size}, tuitest.Keys(w.keys...)...)...)
				checkHeight(t, tm.(Model), "")
				for _, to := range heights {
					resized, _ := tm.Update(tea.WindowSizeMsg{Width: 120, Height: to - 4})
					checkHeight(t, resized.(Model), fmt.Sprintf("after resize to %d: ", to))
				}
			})
		}
	}
}

// checkHeight asserts the frame of m fills the body as TestFillsHeight
// describes; prefix names the step in the failure.
func checkHeight(t *testing.T, m Model, prefix string) {
	t.Helper()
	f := tuitest.Frame(m)
	lines := strings.Split(strings.TrimSuffix(f, "\n"), "\n")
	if got := len(lines); got != m.height {
		t.Errorf("%sframe has %d lines, want %d:\n%s", prefix, got, m.height, f)
	}
	rows := len(dayRow.FindAllString(f, -1))
	if want := min(len(m.shown.days), m.rowsAvail()); rows != want {
		t.Errorf("%sframe shows %d day rows, want %d:\n%s", prefix, rows, want, f)
	}
	empty := 0
	for _, l := range lines {
		if l == "" {
			empty++
		}
	}
	if rows < len(m.shown.days) && empty > 0 {
		t.Errorf("%s%d empty lines while %d of %d day rows are hidden:\n%s", prefix, empty, len(m.shown.days)-rows, len(m.shown.days), f)
	}
	if m.offset > 0 && m.offset+rows != len(m.shown.days) && empty > 0 {
		t.Errorf("%sscrolled to row %d with %d empty lines:\n%s", prefix, m.offset+1, empty, f)
	}
}
