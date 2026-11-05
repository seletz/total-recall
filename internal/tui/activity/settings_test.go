package activity

import (
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/holiday"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// TestSetSettings: changed settings apply to the shown window in place.
// The working time redraws without a query, the day boundary and the
// holidays load the window again, the modes switch like their keys; the
// scroll position of a month stays in every case.
func TestSetSettings(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	base := Settings{DayStart: 4 * time.Hour, Work: work, Holidays: holidays, HumanOnly: true}
	half := work
	half.Target = 4 * time.Hour
	tests := []struct {
		name     string
		settings Settings
		loads    int32 // queries after the start
		want     []string
		lacks    []string
	}{
		{"unchanged", base, 0, []string{"× 8h00m", "human only"}, nil},
		{"working time redraws", with(base, func(s *Settings) { s.Work = half }), 0, []string{"× 4h00m"}, []string{"× 8h00m"}},
		{"no working time", with(base, func(s *Settings) { s.Work = store.WorkHours{} }), 0, nil, []string{"target"}},
		{"holidays reload", with(base, func(s *Settings) { s.Holidays = nil }), 1, nil, []string{"Epiphanias"}},
		{"other region reloads", with(base, func(s *Settings) { s.Holidays = holiday.New("Berlin") }), 1, nil, []string{"Epiphanias"}},
		{"day boundary reloads", with(base, func(s *Settings) { s.DayStart = 0 }), 1, []string{"00      04"}, nil},
		{"all events", with(base, func(s *Settings) { s.HumanOnly = false }), 1, []string{"all events"}, []string{"human only"}},
		{"by project", with(base, func(s *Settings) { s.ByProject = true }), 1, []string{"[projects]", "projects:"}, nil},
		{"newest at the top", with(base, func(s *Settings) { s.Newest = NewestTop }), 0, []string{"day ↑"}, []string{"day ↓"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &countingLoader{st: st}
			m := drive(t, newModel(t, l), "m", "j", "j")
			if m.offset != 2 {
				t.Fatalf("offset = %d before the change", m.offset)
			}
			before := l.calls.Load()
			got := tuitest.Send(t, m, nil).(Model) // no-op, keeps the type
			mm, cmd := got.SetSettings(tt.settings)
			got = tuitest.Run(t, mm, cmd).(Model)
			if n := l.calls.Load() - before; n != tt.loads {
				t.Errorf("loads = %d, want %d", n, tt.loads)
			}
			// The newest day at the top mirrors the scroll position, so the
			// rows on screen stay the same.
			offset := 2
			if tt.settings.Newest == NewestTop {
				offset = m.maxOffset() - 2
			}
			if got.offset != offset || !got.span.equal(m.span) {
				t.Errorf("offset = %d, want %d; span changed = %v", got.offset, offset, !got.span.equal(m.span))
			}
			f := tuitest.Frame(got)
			for _, w := range tt.want {
				if !strings.Contains(f, w) {
					t.Errorf("frame lacks %q:\n%s", w, f)
				}
			}
			for _, b := range tt.lacks {
				if strings.Contains(f, b) {
					t.Errorf("frame has %q:\n%s", b, f)
				}
			}
		})
	}
}

func with(s Settings, f func(*Settings)) Settings {
	f(&s)
	return s
}

// TestSetKeys: new bindings act at once and show in the help.
func TestSetKeys(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	km, err := keys.Resolve(map[string][]string{keys.ActivityEarlier: {"<"}})
	if err != nil {
		t.Fatal(err)
	}
	m := drive(t, New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation}))
	start := m.span
	m = m.SetKeys(km).(Model)
	if got := drive(t, m, "["); !got.span.equal(start) {
		t.Error("[ still shifts the window")
	}
	if got := drive(t, m, "<"); !got.span.equal(start.periods(-1)) {
		t.Errorf("< does not shift: %v", got.span)
	}
	if h := m.keys.Range.Help(); h.Key != "</→" {
		t.Errorf("range group help = %q", h.Key)
	}
}
