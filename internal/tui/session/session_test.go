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

// probe wraps the view so a test can wait until a load result or a window
// size has been applied before it sends the next message.
type probe struct {
	Model
	loaded chan struct{}
	sized  chan struct{}
}

func (p probe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := p.Model.Update(msg)
	p.Model = m.(Model)
	switch msg.(type) {
	case loadedMsg:
		p.loaded <- struct{}{}
	case tea.WindowSizeMsg:
		p.sized <- struct{}{}
	}
	return p, cmd
}

// start runs the view in a test program and waits for the start-up window
// size. Both teatest and the program send one, the program's from a
// goroutine, so a resize sent before both arrived could be overridden.
func start(t *testing.T) (probe, *teatest.TestModel, *store.Store) {
	t.Helper()
	st, _ := fixturedb.Activity(t)
	p := probe{Model: New(st, testfixture.ActivityLocation), loaded: make(chan struct{}, 16), sized: make(chan struct{}, 16)}
	tm := tuitest.New(t, p)
	for range 2 {
		wait(t, p.sized, "initial window size")
	}
	return p, tm, st
}

func wait(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(tuitest.Timeout):
		t.Fatalf("%s did not arrive", what)
	}
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

// TestFrames opens a session, drives the view with steps and compares the
// final frame with a golden file. Steps are key names (see tuitest.Key),
// "open" (send the test's Open and wait for it; implied as the first step
// when absent), "wait" (block until a load result has been applied) and
// "resize:W:H".
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
			// them on by itself (second load).
			return Open{SessionID: alpha4, EventID: eventID(t, st, testfixture.ActivitySessionAlpha4, true, 5), Terms: []string{"vet"}}
		}, []string{"resize:100:12", "open", "wait"}},
		{"JumpToEventWithHits", func(st *store.Store) Open {
			return Open{SessionID: alpha4, EventID: eventID(t, st, testfixture.ActivitySessionAlpha4, false, 2), Terms: []string{"build", "vet"}}
		}, nil},
		{"Scroll", func(*store.Store) Open { return Open{SessionID: alpha4} }, []string{"resize:100:12", "down", "down", "down"}},
		{"ScrollPage", func(*store.Store) Open { return Open{SessionID: alpha4} }, []string{"resize:100:12", "f"}},
		{"OpenUnknownEvent", func(*store.Store) Open {
			// Tools are switched on to look for it (second load); still
			// absent, nothing is marked.
			return Open{SessionID: alpha4, EventID: "claude:nope"}
		}, []string{"open", "wait"}},
		{"ScrollBottom", func(*store.Store) Open { return Open{SessionID: alpha4} }, []string{"resize:100:12", "G"}},
		{"ScrollBottomTop", func(*store.Store) Open { return Open{SessionID: alpha4} }, []string{"resize:100:12", "G", "g"}},
		{"Tools", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySessionBeta1)} }, []string{"T", "wait"}},
		{"ToolsOff", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySessionBeta1)} }, []string{"T", "wait", "T", "wait"}},
		{"ToolsKeepEvent", func(st *store.Store) Open {
			return Open{SessionID: alpha4, EventID: eventID(t, st, testfixture.ActivitySessionAlpha4, false, 2), Terms: []string{"vet"}}
		}, []string{"resize:100:12", "T", "wait"}},
		{"ToolsOffHidesCurrent", func(st *store.Store) Open {
			// The current event is a tool result; without tools the view falls
			// back to the last visible event before it.
			return Open{SessionID: alpha4, EventID: eventID(t, st, testfixture.ActivitySessionAlpha4, true, 2)}
		}, []string{"resize:100:12", "T", "wait", "T", "wait"}},
		{"Copy", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySessionBeta1)} }, []string{"y"}},
		{"CopyNoticeClears", func(*store.Store) Open { return Open{SessionID: sessionID(testfixture.ActivitySessionBeta1)} }, []string{"y", "down"}},
		{"Narrow", func(*store.Store) Open { return Open{SessionID: alpha4, Terms: []string{"findings"}} }, []string{"resize:40:20"}},
		{"NarrowWrap", func(*store.Store) Open {
			return Open{SessionID: sessionID(testfixture.ActivitySessionAlpha3), Terms: []string{"defaults"}}
		}, []string{"resize:24:14"}},
		{"Tiny", func(*store.Store) Open { return Open{SessionID: alpha4} }, []string{"resize:30:3"}},
		{"BeforeOpen", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, tm, st := start(t)
			steps := tt.steps
			if tt.open != nil && !slices.Contains(steps, "open") {
				steps = append([]string{"open"}, steps...)
			}
			for _, s := range steps {
				switch {
				case s == "open":
					tm.Send(tt.open(st))
					wait(t, p.loaded, "load")
				case s == "wait":
					wait(t, p.loaded, "load")
				case strings.HasPrefix(s, "resize:"):
					f := strings.Split(s, ":")
					w, _ := strconv.Atoi(f[1])
					h, _ := strconv.Atoi(f[2])
					tuitest.Resize(tm, w, h)
					wait(t, p.sized, "resize")
				default:
					tuitest.Press(tm, s)
				}
			}
			final := tuitest.Final(t, tm).(probe)
			golden.RequireEqual(t, tuitest.Frame(final))
		})
	}
}

func TestEscSendsBack(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	m := New(st, time.UTC)
	_, cmd := m.Update(tuitest.Key("esc"))
	if cmd == nil {
		t.Fatal("esc returned no command")
	}
	if _, ok := cmd().(Back); !ok {
		t.Errorf("esc produced %T, want Back", cmd())
	}
}

// TestCopyWritesOSC52 checks that y sets the clipboard through OSC 52.
func TestCopyWritesOSC52(t *testing.T) {
	p, tm, _ := start(t)
	id := sessionID(testfixture.ActivitySessionBeta1)
	tm.Send(Open{SessionID: id})
	wait(t, p.loaded, "load")
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
	m := New(st, time.UTC)
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
	m := New(nil, time.UTC)
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
