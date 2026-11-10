package activity

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// TestFillsHeight checks the rows fill the body (#173): at every height,
// for every window, with the newest day at the bottom and at the top, with
// and without the figures and the legend, at both scroll edges and after
// a resize, the frame has exactly the body's lines and as many day rows as
// fit, no line stays empty, and the window's rows carry the marker: all
// of them while the window fits, otherwise as many as fit.
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
		{"WeekNewestTop", []string{"o"}},
		{"WeekNewestTopSliding", []string{"o", "j", "j", "j"}},
		{"Month", []string{"m", "l"}},
		{"MonthEnd", append([]string{"m", "l"}, repeat("j", 40)...)},
		{"MonthNoFigures", []string{"m", "l", "s"}},
		{"MonthProjectsEnd", append([]string{"m", "l", "p"}, repeat("j", 40)...)},
		{"MonthProjectsNoFiguresEnd", append([]string{"m", "l", "p", "s"}, repeat("j", 40)...)},
		{"MonthFiguresBackOn", []string{"m", "l", "s", "s"}},
		{"MonthProjectsOff", []string{"m", "l", "p", "p"}},
		{"MonthNewestTop", []string{"m", "l", "o"}},
		{"MonthNewestTopEnd", append([]string{"m", "l", "o"}, repeat("j", 40)...)},
		{"MonthNewestTopProjectsEnd", append([]string{"m", "l", "o", "p"}, repeat("j", 40)...)},
	}
	// Heights of the terminal; the body is four lines shorter. Every case
	// is resized to each of them afterwards.
	heights := []int{24, 40, 60}
	st, _ := fixturedb.Activity(t)
	for _, w := range windows {
		for _, h := range heights {
			t.Run(fmt.Sprintf("%s/%d", w.name, h), func(t *testing.T) {
				size := tea.WindowSizeMsg{Width: 120, Height: h - 4}
				tm := tuitest.Drive(t, newModel(t, st), append([]tea.Msg{size}, tuitest.Keys(w.keys...)...)...)
				checkHeight(t, tm.(Model), "")
				for _, to := range heights {
					resized := tuitest.Drive(t, tm, tea.WindowSizeMsg{Width: 120, Height: to - 4})
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
	for i, l := range lines {
		if l == "" {
			t.Errorf("%sline %d is empty:\n%s", prefix, i+1, f)
		}
	}
	if got, want := len(dayRow.FindAllString(f, -1)), m.rowsAvail(); got != want {
		t.Errorf("%sframe shows %d day rows, want %d:\n%s", prefix, got, want, f)
	}
	marked := 0
	for _, l := range lines {
		if strings.HasPrefix(l, marker) || strings.HasPrefix(l, selMarker) {
			marked++
		}
	}
	if want := min(len(m.shown.days), m.rowsAvail()); marked != want {
		t.Errorf("%sframe marks %d rows, want %d:\n%s", prefix, marked, want, f)
	}
}
