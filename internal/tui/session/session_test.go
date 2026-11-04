package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// drive starts a view on st at the test terminal size and applies the steps
// in order, running every command to completion (see tuitest.Drive): a load
// that triggers another (tools switched on for a tool event) is followed to
// the end. Steps are key names (see tuitest.Key), "open" (send open) and
// "resize:W:H".
func drive(t *testing.T, st *store.Store, open Open, steps []string) Model {
	t.Helper()
	msgs := []tea.Msg{tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height}}
	for _, s := range steps {
		switch {
		case s == "open":
			msgs = append(msgs, open)
		case strings.HasPrefix(s, "resize:"):
			f := strings.Split(s, ":")
			w, _ := strconv.Atoi(f[1])
			h, _ := strconv.Atoi(f[2])
			msgs = append(msgs, tea.WindowSizeMsg{Width: w, Height: h})
		default:
			msgs = append(msgs, tuitest.Key(s))
		}
	}
	return tuitest.Drive(t, New(st, testfixture.ActivityLocation, nil), msgs...).(Model)
}

func sessionID(native string) string { return "claude:" + native }

// eventID returns the ID of the n-th (0-based) event of a session, with or
// without tool events.
func eventID(t *testing.T, st *store.Store, native string, all bool, n int) string {
	t.Helper()
	evs, err := st.Events(context.Background(), sessionID(native), all)
	if err != nil {
		t.Fatal(err)
	}
	if n >= len(evs) {
		t.Fatalf("session %s has %d events, want index %d", native, len(evs), n)
	}
	return evs[n].ID
}

// TestFrames opens a session, drives the view with steps (see drive; "open"
// is implied as the first step when absent) and compares the final frame
// with a golden file. Commands run synchronously, so every frame shows the
// settled state: an Open that needs a second load (tool event) is complete
// before the next step.
func TestFrames(t *testing.T) {
	alpha4 := sessionID(testfixture.ActivitySessionAlpha4)
	tests := []struct {
		name  string
		open  func(st *store.Store) Open
		steps []string
	}{
		{"Open", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySessionBeta1)} }, nil},
		{"OpenPrefix", func(*store.Store) Open { return Open{SessionID: "b2b2b2b2-0001"} }, nil},
		{"OpenNotFound", func(*store.Store) Open { return Open{SessionID: "nope"} }, nil},
		{"OpenSubagent", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySubagent)} }, nil},
		{"Hits", func(*store.Store) Open { return Open{SessionID: alpha4, Terms: []string{"build", "vet"}} }, nil},
		{"NoHits", func(*store.Store) Open { return Open{SessionID: alpha4, Terms: []string{"zzz"}} }, nil},
		{"NextHit", func(*store.Store) Open { return Open{SessionID: alpha4, Terms: []string{"build", "vet"}} }, []string{"n"}},
		{"NextHitWraps", func(*store.Store) Open { return Open{SessionID: alpha4, Terms: []string{"build", "vet"}} }, []string{"n", "n", "n"}},
		{"PrevHitWraps", func(*store.Store) Open { return Open{SessionID: alpha4, Terms: []string{"build", "vet"}} }, []string{"N"}},
		{"HitKeysWithoutTerms", func(*store.Store) Open { return Open{SessionID: alpha4} }, []string{"n", "N"}},
		{"JumpToEvent", func(st *store.Store) Open {
			return Open{SessionID: alpha4, EventID: eventID(t, st, testfixture.ActivitySessionAlpha4, false, 4)}
		}, []string{"resize:100:12", "open"}},
		{"JumpToEventThenResize", func(st *store.Store) Open {
			return Open{SessionID: alpha4, EventID: eventID(t, st, testfixture.ActivitySessionAlpha4, false, 4)}
		}, []string{"resize:100:12"}},
		{"OpenToolEvent", func(st *store.Store) Open {
			// A tool event is only listed with tools on: the view switches
			// them on by itself with a second load.
			return Open{SessionID: alpha4, EventID: eventID(t, st, testfixture.ActivitySessionAlpha4, true, 5), Terms: []string{"vet"}}
		}, []string{"resize:100:12", "open"}},
		{"JumpToEventWithHits", func(st *store.Store) Open {
			return Open{SessionID: alpha4, EventID: eventID(t, st, testfixture.ActivitySessionAlpha4, false, 2), Terms: []string{"build", "vet"}}
		}, nil},
		{"Scroll", func(*store.Store) Open { return Open{SessionID: alpha4} }, []string{"resize:100:12", "down", "down", "down"}},
		{"ScrollPage", func(*store.Store) Open { return Open{SessionID: alpha4} }, []string{"resize:100:12", "f"}},
		{"OpenUnknownEvent", func(*store.Store) Open {
			// Tools are switched on to look for it with a second load; still
			// absent, nothing is marked.
			return Open{SessionID: alpha4, EventID: "claude:nope"}
		}, []string{"open"}},
		{"ScrollBottom", func(*store.Store) Open { return Open{SessionID: alpha4} }, []string{"resize:100:12", "G"}},
		{"ScrollBottomTop", func(*store.Store) Open { return Open{SessionID: alpha4} }, []string{"resize:100:12", "G", "g"}},
		{"Tools", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySessionBeta1)} }, []string{"T"}},
		{"ToolsOff", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySessionBeta1)} }, []string{"T", "T"}},
		{"ToolsKeepEvent", func(st *store.Store) Open {
			return Open{SessionID: alpha4, EventID: eventID(t, st, testfixture.ActivitySessionAlpha4, false, 2), Terms: []string{"vet"}}
		}, []string{"resize:100:12", "T"}},
		{"ToolsOffHidesCurrent", func(st *store.Store) Open {
			// Opening a tool result switches tools on and marks it; T switches
			// them off, and the view falls back to the last visible event
			// before it.
			return Open{SessionID: alpha4, EventID: eventID(t, st, testfixture.ActivitySessionAlpha4, true, 2)}
		}, []string{"resize:100:12", "T"}},
		{"Copy", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySessionBeta1)} }, []string{"y"}},
		{"CopyNoticeClears", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySessionBeta1)} }, []string{"y", "down"}},
		{"Narrow", func(*store.Store) Open { return Open{SessionID: alpha4, Terms: []string{"findings"}} }, []string{"resize:40:20"}},
		{"NarrowWrap", func(*store.Store) Open {
			return Open{SessionID: sessionID(testfixture.ActivitySessionAlpha3), Terms: []string{"defaults"}}
		}, []string{"resize:24:14"}},
		{"Tiny", func(*store.Store) Open { return Open{SessionID: alpha4} }, []string{"resize:30:3"}},
		{"BeforeOpen", nil, nil},
		// Gamma1 was continued in Gamma2: both show the link on a third
		// header line, which shrinks the viewport.
		{"ContinuedIn", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySessionGamma1)} }, nil},
		{"ContinuedFrom", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySessionGamma2)} }, []string{"resize:100:9"}},
		{"ContinuedNarrow", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySessionGamma2)} }, []string{"resize:40:12"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := tt.steps
			var open Open
			if tt.open != nil {
				open = tt.open(st)
				if !slices.Contains(steps, "open") {
					steps = append([]string{"open"}, steps...)
				}
			}
			m := drive(t, st, open, steps)
			golden.RequireEqual(t, tuitest.Frame(m))
		})
	}
}

func TestEscSendsBack(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := New(st, time.UTC, nil)
	_, cmd := m.Update(tuitest.Key("esc"))
	if cmd == nil {
		t.Fatal("esc returned no command")
	}
	if _, ok := cmd().(Back); !ok {
		t.Errorf("esc produced %T, want Back", cmd())
	}
}

// TestCopyWritesOSC52 checks that y sets the clipboard through OSC 52. It
// runs a real program, since the sequence is written by the renderer.
func TestCopyWritesOSC52(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	tm := tuitest.New(t, New(st, testfixture.ActivityLocation, nil))
	id := sessionID(testfixture.ActivitySessionBeta1)
	tm.Send(Open{SessionID: id})
	// The header shows the ID once the session is loaded; y is ignored
	// before that.
	tuitest.WaitFor(t, tm, id)
	tuitest.Press(tm, "y")
	// The sequence is queued and flushed with the next frame, so wait for it
	// instead of reading the output after quitting.
	want := []byte("\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(id)))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return bytes.Contains(b, want) },
		teatest.WithDuration(tuitest.Timeout))
	tuitest.Final(t, tm)
}

// TestStaleLoadDiscarded opens two sessions in a row; the result of the
// first load arrives last and must not replace the second session.
func TestStaleLoadDiscarded(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := New(st, time.UTC, nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	first := sessionID(testfixture.ActivitySessionBeta1)
	second := sessionID(testfixture.ActivitySessionGamma1)
	var cmds [2]tea.Cmd
	var mm tea.Model = m
	mm, cmds[0] = mm.Update(Open{SessionID: first})
	mm, cmds[1] = mm.Update(Open{SessionID: second})
	mm, _ = mm.Update(cmds[1]())
	mm, _ = mm.Update(cmds[0]())
	if got := mm.(Model).SessionID(); got != second {
		t.Errorf("session after stale load = %q, want %q", got, second)
	}
}

func TestHelp(t *testing.T) {
	m := New(nil, time.UTC, nil)
	if m.Title() != "session" {
		t.Errorf("Title = %q", m.Title())
	}
	if len(m.ShortHelp()) == 0 || len(m.FullHelp()) == 0 {
		t.Error("help is empty")
	}
	for _, b := range m.ShortHelp() {
		if b.Help().Key == "" || b.Help().Desc == "" {
			t.Errorf("binding %v without help text", b.Keys())
		}
	}
}

// TestWheel checks that the mouse wheel scrolls the transcript; the view is
// low enough that the fixture session does not fit.
func TestWheel(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := drive(t, st, Open{SessionID: sessionID(testfixture.ActivitySessionAlpha4)}, []string{"open", "resize:100:8"})
	down := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown}
	up := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp}
	got := tuitest.Drive(t, m, down, down, up).(Model)
	if got.vp.YOffset() != m.vp.MouseWheelDelta {
		t.Errorf("YOffset = %d, want %d", got.vp.YOffset(), m.vp.MouseWheelDelta)
	}
}
