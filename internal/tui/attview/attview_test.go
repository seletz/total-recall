package attview

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// fakeOpener records what the view asked to open.
type fakeOpener struct {
	name string
	data []byte
	err  error
}

func (f *fakeOpener) Open(name string, data []byte) (string, error) {
	f.name, f.data = name, data
	return "/tmp/x/" + name, f.err
}

// demoEvent returns the session of the demo archive that carries the
// attachments (an image and a document, each on an event of its own) and
// the event of the image.
func demoEvent(t *testing.T, st *store.Store) (session, event string) {
	t.Helper()
	hits, err := st.SearchTyped(context.Background(), store.TypedQuery{Text: "image", Types: []store.HitType{store.TypeEvent}})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range hits {
		for _, h := range g.Hits {
			if a, _ := st.EventsAttachments(context.Background(), []string{h.Event.Event.ID}); len(a) > 0 {
				return h.Event.Event.SessionID, h.Event.Event.ID
			}
		}
	}
	t.Fatal("no event with attachments in the demo archive")
	return "", ""
}

func opened(t *testing.T, w, h int) (Model, *fakeOpener) {
	t.Helper()
	st, _ := fixturedb.Demo(t)
	op := &fakeOpener{}
	m := New(Options{Loader: st, Location: demo.Location, External: op})
	var tm tea.Model = m
	tm = tuitest.Send(t, tm, tea.WindowSizeMsg{Width: w, Height: h})
	sid, eid := demoEvent(t, st)
	tm = tuitest.Send(t, tm, Open{SessionID: sid, EventID: eid, Title: "Höhenprofil prüfen"})
	return tm.(Model), op
}

func frame(m Model) string { return ansi.Strip(m.View().Content) }

func send(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	var tm tea.Model = m
	for _, msg := range msgs {
		tm = tuitest.Send(t, tm, msg)
	}
	return tm.(Model)
}

func TestOpenShowsTheFirstAttachment(t *testing.T) {
	m, _ := opened(t, 100, 30)
	got := frame(m)
	for _, want := range []string{"Höhenprofil prüfen", "1 image/png", "2 application/pdf", "png 96×48", "total-recall blob", "1 of 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("view lacks %q:\n%s", want, got)
		}
	}
	if !strings.ContainsAny(got, "░▒▓█") {
		t.Errorf("the image is drawn as shades:\n%s", got)
	}
}

func TestBrowse(t *testing.T) {
	m, _ := opened(t, 100, 30)
	tests := []struct {
		name string
		keys []string
		want string // what the view shows after the keys
		not  string
	}{
		{"next shows the document", []string{"right"}, "Notes on the track format", "png 96×48"},
		{"next key n", []string{"n"}, "Notes on the track format", ""},
		{"stops at the last", []string{"right", "right"}, "Notes on the track format", ""},
		{"previous at the first stays", []string{"left"}, "png 96×48", ""},
		{"there and back", []string{"right", "left"}, "png 96×48", "Notes on the track"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msgs []tea.Msg
			for _, k := range tt.keys {
				msgs = append(msgs, tuitest.Key(k))
			}
			got := frame(send(t, m, msgs...))
			if !strings.Contains(got, tt.want) {
				t.Errorf("lacks %q:\n%s", tt.want, got)
			}
			if tt.not != "" && strings.Contains(got, tt.not) {
				t.Errorf("has %q:\n%s", tt.not, got)
			}
		})
	}
}

func TestOpenExternal(t *testing.T) {
	m, op := opened(t, 100, 30)
	m = send(t, m, tuitest.Key("o"))
	if !strings.HasSuffix(op.name, ".png") || len(op.data) == 0 {
		t.Errorf("opened %q with %d bytes, want the image as .png", op.name, len(op.data))
	}
	if got := frame(m); !strings.Contains(got, "opened /tmp/x/") {
		t.Errorf("status lacks the path:\n%s", got)
	}
	m = send(t, m, tuitest.Key("right"), tuitest.Key("enter"))
	if !strings.HasSuffix(op.name, ".pdf") {
		t.Errorf("second attachment opened as %q", op.name)
	}
}

func TestOpenExternalFails(t *testing.T) {
	m, op := opened(t, 100, 30)
	op.err = errors.New("no such program")
	got := frame(send(t, m, tuitest.Key("o")))
	if !strings.Contains(got, "cannot open: no such program") || !strings.Contains(got, "total-recall blob") {
		t.Errorf("failure is shown with the fallback:\n%s", got)
	}
}

func TestBackKey(t *testing.T) {
	m, _ := opened(t, 100, 30)
	_, cmd := m.Update(tuitest.Key("esc"))
	if cmd == nil {
		t.Fatal("esc returns a command")
	}
	if _, ok := cmd().(Back); !ok {
		t.Errorf("esc sends Back")
	}
}

func TestNoAttachments(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	m := New(Options{Loader: st, Location: demo.Location})
	var tm tea.Model = m
	tm = tuitest.Send(t, tm, tea.WindowSizeMsg{Width: 80, Height: 20})
	tm = tuitest.Send(t, tm, Open{SessionID: "claude:nonexistent", Title: "x"})
	if got := frame(tm.(Model)); !strings.Contains(got, "no attachments") {
		t.Errorf("empty view:\n%s", got)
	}
}

func TestNoDatabase(t *testing.T) {
	m := New(Options{})
	var tm tea.Model = m
	tm = tuitest.Send(t, tm, tea.WindowSizeMsg{Width: 80, Height: 20})
	tm = tuitest.Send(t, tm, Open{SessionID: "s", Title: "x"})
	if got := frame(tm.(Model)); !strings.Contains(got, "no database") {
		t.Errorf("view:\n%s", got)
	}
}

func TestSizes(t *testing.T) {
	// Every size keeps every line inside the width and the view inside the
	// height, down to a terminal of 1×1.
	for _, d := range []struct{ w, h int }{{100, 30}, {60, 20}, {40, 12}, {24, 8}, {10, 4}, {1, 1}, {3, 2}} {
		m, _ := opened(t, d.w, d.h)
		out := m.View().Content
		lines := strings.Split(out, "\n")
		if len(lines) > d.h {
			t.Errorf("%dx%d: %d lines", d.w, d.h, len(lines))
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > d.w {
				t.Errorf("%dx%d: line of width %d: %q", d.w, d.h, ansi.StringWidth(l), ansi.Strip(l))
			}
		}
	}
}

func TestMouse(t *testing.T) {
	m, _ := opened(t, 100, 30)
	// The second tab sits on row 1 of the view; find its column from the frame.
	lines := strings.Split(frame(m), "\n")
	col := strings.Index(lines[1], "2 application/pdf")
	if col < 0 {
		t.Fatalf("no second tab in %q", lines[1])
	}
	m = send(t, m, tea.MouseClickMsg{X: col + 2, Y: 1, Button: tea.MouseLeft})
	if got := frame(m); !strings.Contains(got, "Notes on the track format") {
		t.Errorf("click on the tab shows the document:\n%s", got)
	}
	// A click elsewhere changes nothing; the wheel on a short body neither breaks.
	m = send(t, m, tea.MouseClickMsg{X: 5, Y: 10, Button: tea.MouseLeft}, tea.MouseWheelMsg{X: 5, Y: 10, Button: tea.MouseWheelDown})
	if got := frame(m); !strings.Contains(got, "Notes on the track format") {
		t.Errorf("still the document:\n%s", got)
	}
}

func TestLateAnswerIsDropped(t *testing.T) {
	m, _ := opened(t, 100, 30)
	// An answer of an older Open must not replace the list.
	stale := loadedMsg{seq: m.seq - 1, atts: nil}
	m = send(t, m, stale)
	if got := frame(m); !strings.Contains(got, "png 96×48") {
		t.Errorf("stale answer applied:\n%s", got)
	}
}

func TestKeysCanBeRebound(t *testing.T) {
	m, op := opened(t, 100, 30)
	km, err := keys.Resolve(map[string][]string{"attach_open": {"x"}})
	if err != nil {
		t.Fatal(err)
	}
	m = m.SetKeys(km).(Model)
	m = send(t, m, tuitest.Key("o"))
	if op.name != "" {
		t.Errorf("o no longer opens: %q", op.name)
	}
	m = send(t, m, tuitest.Key("x"))
	if op.name == "" {
		t.Error("x opens")
	}
}

func TestOpenMarksTheAttachmentOfTheEvent(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	sid, _ := demoEvent(t, st)
	list, err := st.SessionAttachmentList(context.Background(), sid)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	tests := []struct {
		name  string
		event string
		want  string
	}{
		{"event of the document", list[1].EventID, "Notes on the track format"},
		{"event of the image", list[0].EventID, "png 96×48"},
		{"event without attachments", "claude:other", "png 96×48"},
		{"no event", "", "png 96×48"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tm tea.Model = New(Options{Loader: st, Location: demo.Location})
			tm = tuitest.Send(t, tm, tea.WindowSizeMsg{Width: 100, Height: 30})
			tm = tuitest.Send(t, tm, Open{SessionID: sid, EventID: tt.event, Title: "x"})
			if got := frame(tm.(Model)); !strings.Contains(got, tt.want) {
				t.Errorf("lacks %q:\n%s", tt.want, got)
			}
		})
	}
}
