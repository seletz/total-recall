package tui

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

func TestParseGraphics(t *testing.T) {
	tests := []struct {
		in      string
		want    Graphics
		wantErr bool
	}{
		{"", GraphicsAuto, false},
		{"auto", GraphicsAuto, false},
		{"text", GraphicsText, false},
		{"kitty", GraphicsKitty, false},
		{"Text", "", true},
		{"sixel", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseGraphics(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseGraphics(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseGraphics(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewRejectsUnknownView(t *testing.T) {
	if _, err := New(Options{Start: "bogus"}); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("New(Start: bogus) error = %v", err)
	}
}

// TestFrames drives the app with key presses and compares the final frame
// (the rendered View, without escape sequences) against a golden file.
func TestFrames(t *testing.T) {
	tests := []struct {
		name  string
		start string
		keys  []string
	}{
		// The search view starts with its input focused: the first tab moves
		// the focus to the hit list, only then do the global keys apply. Tab
		// cycles the focus within the search view and never switches views.
		{"Start", "", nil},
		{"StartStats", "stats", nil},
		{"StartActivity", "activity", nil},
		{"TypedIntoSearch", "", []string{"2", "q", "?"}},
		{"Key2", "", []string{"tab", "2"}},
		{"Key1AfterStats", "stats", []string{"1"}},
		{"Key3", "", []string{"tab", "3"}},
		{"Key2AfterActivity", "activity", []string{"2"}},
		{"Key1AfterActivity", "activity", []string{"1"}},
		{"TabInActivityIgnored", "activity", []string{"tab"}},
		{"TabCyclesFocus", "", []string{"tab", "tab"}},
		{"TabTabThen2IsText", "", []string{"tab", "tab", "2"}},
		{"TabInStatsIgnored", "stats", []string{"tab"}},
		{"ShiftTabToList", "", []string{"shift+tab", "2"}},
		{"HelpOpen", "", []string{"tab", "?"}},
		{"HelpOpenStats", "stats", []string{"?"}},
		{"HelpOpenActivity", "activity", []string{"?"}},
		{"HelpToggleOff", "", []string{"tab", "?", "?"}},
		{"HelpEscCloses", "", []string{"tab", "?", "esc"}},
		{"HelpSwallowsKeys", "", []string{"tab", "?", "2", "tab"}},
		{"Resize", "", []string{"resize"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := New(Options{Start: tt.start, Now: tuitest.Clock})
			if err != nil {
				t.Fatal(err)
			}
			tm := tuitest.New(t, app)
			for _, k := range tt.keys {
				if k == "resize" {
					// Wait for the first frame so the initial size has been
					// processed before the new one arrives.
					tuitest.WaitFor(t, tm, "total-recall")
					tuitest.Resize(tm, 60, 12)
					continue
				}
				tuitest.Press(tm, k)
			}
			final := tuitest.Final(t, tm).(App)
			golden.RequireEqual(t, tuitest.Frame(final))
		})
	}
}

// TestOutput is the teatest basis: the raw renderer output of a start-up.
func TestOutput(t *testing.T) {
	app, err := New(Options{Now: tuitest.Clock})
	if err != nil {
		t.Fatal(err)
	}
	tm := tuitest.New(t, app)
	tuitest.RequireOutput(t, tm)
}

func TestQuit(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{"q from list", []string{"tab", "q"}},
		{"q from stats", []string{"tab", "2", "q"}},
		{"q from activity", []string{"tab", "3", "q"}},
		{"ctrl+c", []string{"ctrl+c"}},
		{"ctrl+c in search input", []string{"x", "ctrl+c"}},
		{"ctrl+c in help", []string{"tab", "?", "ctrl+c"}},
		{"q in help", []string{"tab", "?", "q"}},
		{"q after help", []string{"tab", "?", "esc", "q"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := New(Options{Now: tuitest.Clock})
			if err != nil {
				t.Fatal(err)
			}
			tm := tuitest.New(t, app)
			tuitest.Press(tm, tt.keys...)
			// Fails with a timeout if the program does not quit by itself.
			tuitest.WaitQuit(t, tm)
		})
	}
}

// TestSearchWiring checks that New hands the initial query to the search
// view: it shows up in the input, and without a store the view says so.
func TestSearchWiring(t *testing.T) {
	app, err := New(Options{Query: "release", Now: tuitest.Clock})
	if err != nil {
		t.Fatal(err)
	}
	tm := tuitest.New(t, app)
	final := tuitest.Final(t, tm).(App)
	frame := tuitest.Frame(final)
	for _, want := range []string{"> release", "no database"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame lacks %q:\n%s", want, frame)
		}
	}
}

// TestActivityWiring checks that New hands store, clock and day boundary to
// the activity view: with the day beginning at 04:00 the fixture's New
// Year's Eve session is one day ending past midnight.
func TestActivityWiring(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	now := time.Date(2026, 1, 11, 12, 0, 0, 0, testfixture.ActivityLocation)
	app, err := New(Options{Store: st, Start: ViewActivity, Location: testfixture.ActivityLocation,
		Now: func() time.Time { return now }, DayStart: 4 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	tm := tuitest.New(t, app)
	tuitest.WaitFor(t, tm, "total:")
	tuitest.Press(tm, "l")
	tuitest.WaitFor(t, tm, "last week")
	final := tuitest.Final(t, tm).(App)
	frame := tuitest.Frame(final)
	for _, want := range []string{"[3 activity]", "last week", "23:40  00:05 (+1)", "total: 2 days with activity, 25m"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame lacks %q:\n%s", want, frame)
		}
	}
}

// TestCursorBelowHeader checks that the app shifts the search input's cursor
// past the header and the rule, and drops it while the help is open.
func TestCursorBelowHeader(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want bool
	}{
		{"input focused", nil, true},
		{"help open", []string{"tab", "?"}, false},
		{"stats", []string{"tab", "2"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := New(Options{Query: "ab", Now: tuitest.Clock})
			if err != nil {
				t.Fatal(err)
			}
			var m tea.Model = app
			m, _ = m.Update(tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height})
			for _, k := range tt.keys {
				m, _ = m.Update(tuitest.Key(k))
			}
			c := m.View().Cursor
			if (c != nil) != tt.want {
				t.Fatalf("cursor present = %v, want %v", c != nil, tt.want)
			}
			if c != nil && (c.X != 5 || c.Y != 2) {
				t.Errorf("cursor = (%d,%d), want (5,2)", c.X, c.Y)
			}
		})
	}
}

// appProbe wraps the app so a test can wait until the detail has loaded
// its session and until the key presses sent so far have been handled.
type appProbe struct {
	App
	st *probeState
}

type probeState struct {
	mu     sync.Mutex
	keys   int  // key presses handled
	loaded bool // the detail holds a session
	shown  bool // the detail is shown
}

func (p appProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := p.App.Update(msg)
	p.App = m.(App)
	p.st.mu.Lock()
	defer p.st.mu.Unlock()
	if _, ok := msg.(tea.KeyPressMsg); ok {
		p.st.keys++
	}
	p.st.loaded = p.detail.SessionID() != ""
	p.st.shown = p.showDetail
	return p, cmd
}

// wait blocks until keys key presses have been handled and, with loaded,
// the detail holds a session, or with closed, the detail is hidden again
// (Back arrives as a message after the esc key press).
func (p *probeState) wait(t *testing.T, keys int, loaded, closed bool) {
	t.Helper()
	deadline := time.Now().Add(tuitest.Timeout)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		ok := p.keys >= keys && (!loaded || p.loaded) && (!closed || !p.shown)
		p.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("app did not settle (keys %d, loaded %v, closed %v)", keys, loaded, closed)
}

// TestDetail drives the round trip search → detail → search against the
// Activity fixture and compares the final frame with a golden file. Steps
// are key names, "wait:<text>" (wait until the output shows text, used for
// the search preview), "loaded" (wait until the detail has loaded a
// session), "closed" (wait until the detail has gone again) and "keys"
// (wait until every key press so far has been handled).
func TestDetail(t *testing.T) {
	tests := []struct {
		name  string
		steps []string
	}{
		{"Open", []string{"wait:Tagged v0.2.0.", "enter", "loaded"}},
		{"OpenFromList", []string{"wait:Tagged v0.2.0.", "tab", "j", "wait:plan the week", "enter", "loaded"}},
		{"BackKeepsInput", []string{"wait:Tagged v0.2.0.", "enter", "loaded", "esc", "closed"}},
		{"BackKeepsList", []string{"wait:Tagged v0.2.0.", "tab", "j", "wait:plan the week", "enter", "loaded", "esc", "closed"}},
		{"Copy", []string{"wait:Tagged v0.2.0.", "enter", "loaded", "y", "keys"}},
		{"Help", []string{"wait:Tagged v0.2.0.", "enter", "loaded", "?", "keys"}},
		{"TabKeepsDetail", []string{"wait:Tagged v0.2.0.", "enter", "loaded", "tab", "keys"}},
		{"Key1Closes", []string{"wait:Tagged v0.2.0.", "tab", "enter", "loaded", "1", "keys"}},
		{"Key2Closes", []string{"wait:Tagged v0.2.0.", "enter", "loaded", "2", "keys"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := New(Options{Store: st, Query: "release", Now: tuitest.Clock, Location: testfixture.ActivityLocation})
			if err != nil {
				t.Fatal(err)
			}
			p := appProbe{App: app, st: &probeState{}}
			tm := tuitest.New(t, p)
			keys := 0
			for _, s := range tt.steps {
				switch {
				case strings.HasPrefix(s, "wait:"):
					tuitest.WaitFor(t, tm, strings.TrimPrefix(s, "wait:"))
				case s == "loaded":
					p.st.wait(t, keys, true, false)
				case s == "closed":
					p.st.wait(t, keys, false, true)
				case s == "keys":
					p.st.wait(t, keys, false, false)
				default:
					tuitest.Press(tm, s)
					keys++
				}
			}
			final := tuitest.Final(t, tm).(appProbe).App
			golden.RequireEqual(t, tuitest.Frame(final))
		})
	}
}

// TestDetailQuit checks that q quits from the detail: it does not hold the
// keyboard the way the search input does.
func TestDetailQuit(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	app, err := New(Options{Store: st, Query: "release", Now: tuitest.Clock, Location: testfixture.ActivityLocation})
	if err != nil {
		t.Fatal(err)
	}
	p := appProbe{App: app, st: &probeState{}}
	tm := tuitest.New(t, p)
	tuitest.WaitFor(t, tm, "Tagged v0.2.0.")
	tuitest.Press(tm, "enter")
	p.st.wait(t, 1, true, false)
	tuitest.Press(tm, "q")
	tuitest.WaitQuit(t, tm)
}
