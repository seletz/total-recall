package search

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// Mouse coordinates are relative to the body the app hands the view: the
// input is row 0, the rule row 1, the first hit starts at row 2; the list
// column spans the left half.
func wheel(x, y int, up bool) tea.MouseWheelMsg {
	b := tea.MouseWheelDown
	if up {
		b = tea.MouseWheelUp
	}
	return tea.MouseWheelMsg{X: x, Y: y, Button: b}
}

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// TestMouse drives the view with mouse messages: the wheel over the list
// moves the mark, over the preview it scrolls the preview; a click marks a
// hit and gives the list the focus, a click on the marked hit opens it.
func TestMouse(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	hits, err := st.Search(context.Background(), "release", store.SearchFilter{})
	if err != nil || len(hits) < 2 {
		t.Fatalf("fixture search: %d hits, %v", len(hits), err)
	}
	const previewX = 70 // in the right column at width 100
	// The body is low enough that the exchange of the first hit does not
	// fit beside the list, so the preview can scroll.
	const height = 9
	up := make([]tea.Msg, 0, 20)
	for range cap(up) {
		up = append(up, wheel(previewX, 5, true))
	}
	// Expected preview scroll: the automatic position, scrolled down from
	// it, or the first line.
	const (
		auto = "auto"
		down = "down"
		top  = "top"
	)
	tests := []struct {
		name   string
		msgs   []tea.Msg
		cursor int
		focus  focus
		scroll string
		open   bool // the last message opened the marked hit
	}{
		{"wheel down marks next", []tea.Msg{wheel(5, 5, false)}, 1, focusInput, auto, false},
		{"wheel up at top stays", []tea.Msg{wheel(5, 5, true)}, 0, focusInput, auto, false},
		{"wheel down then up", []tea.Msg{wheel(5, 5, false), wheel(5, 5, true)}, 0, focusInput, auto, false},
		{"wheel over preview scrolls preview", []tea.Msg{wheel(previewX, 5, false)}, 0, focusInput, down, false},
		{"preview scroll stops at top", append([]tea.Msg{wheel(previewX, 5, false)}, up...), 0, focusInput, top, false},
		{"mark change resets preview scroll", []tea.Msg{wheel(previewX, 5, false), wheel(5, 5, false)}, 1, focusInput, auto, false},
		{"click marks hit and focuses list", []tea.Msg{click(5, 2+linesPerHit)}, 1, focusList, auto, false},
		{"click on snippet line marks hit", []tea.Msg{click(5, 2+linesPerHit+1)}, 1, focusList, auto, false},
		{"click on marked hit opens it", []tea.Msg{click(5, 2), click(5, 2)}, 0, focusList, auto, true},
		{"click on input focuses input", []tea.Msg{tuitest.Key("tab"), click(5, 0)}, 0, focusInput, auto, false},
		{"click below the hits is ignored", []tea.Msg{click(5, height-2)}, 0, focusInput, auto, false},
		{"click in preview is ignored", []tea.Msg{click(previewX, 2)}, 0, focusInput, auto, false},
		{"repo filter ignores the mouse", []tea.Msg{tuitest.Key("tab"), tuitest.Key("r"), click(5, 2+linesPerHit)}, 0, focusRepo, auto, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(t, st, "release")
			m.opts.Debounce = time.Millisecond
			got := tuitest.Drive(t, m, tea.WindowSizeMsg{Width: tuitest.Width, Height: height})
			// Commands run to completion after every message (preview tick
			// and load); OpenSession is kept instead of delivered.
			var opened *OpenSession
			update := func(msg tea.Msg) tea.Cmd {
				if o, ok := msg.(OpenSession); ok {
					opened = &o
					return nil
				}
				var cmd tea.Cmd
				got, cmd = got.Update(msg)
				return cmd
			}
			for _, msg := range tt.msgs {
				opened = nil
				tuitest.Exec(t, update, func() tea.Msg { return msg })
			}
			sm := got.(Model)
			if sm.cursor != tt.cursor {
				t.Errorf("cursor = %d, want %d", sm.cursor, tt.cursor)
			}
			if sm.focus != tt.focus {
				t.Errorf("focus = %d, want %d", sm.focus, tt.focus)
			}
			switch p := sm.preview; {
			case tt.scroll == auto && p.scrolled,
				tt.scroll == top && (!p.scrolled || p.scroll != 0),
				tt.scroll == down && (!p.scrolled || p.scroll <= 0):
				t.Errorf("preview scrolled = %v, scroll = %d, want %s", p.scrolled, p.scroll, tt.scroll)
			}
			open := opened != nil && opened.SessionID == hits[tt.cursor].Event.SessionID
			if open != tt.open {
				t.Errorf("opened = %v, want %v", open, tt.open)
			}
		})
	}
}
