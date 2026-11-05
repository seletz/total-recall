package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/NimbleMarkets/ntcharts/v2/picture"

	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// TestActivityScrollWritesNothing scrolls the activity view through time
// with keys and the wheel in text and in forced kitty mode: besides the
// frame nothing goes to the terminal, so no image is transmitted again
// while the activity view scrolls. The start-up (the stats view places its
// image) is the only thing that may write. Forcing kitty pins the picture
// package's capability for the process; the cleanup restores TestMain's.
func TestActivityScrollWritesNothing(t *testing.T) {
	t.Cleanup(func() { picture.ForceKittyCapability(picture.KittyCapabilityUnsupported) })
	st, _ := fixturedb.Activity(t)
	now := time.Date(2026, 1, 11, 12, 0, 0, 0, testfixture.ActivityLocation)
	for _, g := range []Graphics{GraphicsText, GraphicsKitty} {
		t.Run(string(g), func(t *testing.T) {
			app, err := New(Options{Store: st, Start: ViewActivity, Graphics: g, Now: func() time.Time { return now }, Location: testfixture.ActivityLocation})
			if err != nil {
				t.Fatal(err)
			}
			var m tea.Model = app
			update := func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m, cmd = m.Update(msg)
				return cmd
			}
			tuitest.Exec(t, update, m.Init())
			tuitest.Exec(t, update, update(tea.WindowSizeMsg{Width: tuitest.Width, Height: tuitest.Height}))
			msgs := []tea.Msg{tuitest.Key("k"), tuitest.Key("up"), tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelUp}, tuitest.Key("["), tuitest.Key("j")}
			for i := range 20 {
				msg := msgs[i%len(msgs)]
				if w := tuitest.Exec(t, update, update(msg)); len(w) > 0 {
					t.Errorf("step %d (%T) wrote %d raw chunks to the terminal", i, msg, len(w))
				}
			}
			f := frame(m)
			requireContains(t, f, "[3 activity]", "week:")
			requireLacks(t, f, "Loading", "loading")
		})
	}
}
