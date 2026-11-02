package screenshot

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/holiday"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui"
)

// Sizes of the screenshots in terminal cells.
const (
	Width, Height               = 120, 36
	CompactWidth, CompactHeight = 80, 24
)

// Shot describes one screenshot: which archive it reads, the view it starts
// in, the clock the view sees and the keys pressed before the frame is
// captured. The clock pins the windows of the stats and activity views to
// fixed dates, so a rerun yields the same frame as long as the archive's
// past does not change.
type Shot struct {
	Name          string
	Width, Height int
	// Demo selects the synthetic demo archive; otherwise the anonymized
	// copy of the real archive is read.
	Demo  bool
	Start string
	Query string
	Now   time.Time
	// Keys are pressed in order: single characters, "enter", "tab", "esc".
	Keys []string
	// Caption describes the shot for the docs.
	Caption string
}

// Frame is the rendered final frame of a Shot, with colours.
type Frame struct {
	Shot    Shot
	Content string
}

// Plain returns the frame without escape sequences and trailing blanks.
func (f Frame) Plain() string {
	lines := strings.Split(ansi.Strip(f.Content), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

// berlin is the time zone of every shot.
var berlin = must(time.LoadLocation("Europe/Berlin"))

func must(loc *time.Location, err error) *time.Location {
	if err != nil {
		panic(err)
	}
	return loc
}

// Clocks of the shots. The demo conversations end on 2026-10-31; the stats
// window ends with the week of 2026-11-01 (Allerheiligen, a Sunday) and the
// month shot shows October 2026 (Tag der Deutschen Einheit, a Saturday).
var (
	demoNow  = time.Date(2026, 11, 2, 10, 0, 0, 0, berlin)
	weekNow  = time.Date(2026, 11, 1, 12, 0, 0, 0, berlin)
	monthNow = time.Date(2026, 10, 31, 12, 0, 0, 0, berlin)
)

// Shots are the screenshots of the docs, in the order of docs/reference/30-tui.md.
var Shots = []Shot{
	{Name: "search", Width: Width, Height: Height, Demo: true, Start: tui.ViewSearch, Query: DemoQuery, Now: demoNow,
		Caption: "Suche mit Vorschau"},
	{Name: "session", Width: Width, Height: Height, Demo: true, Start: tui.ViewSearch, Query: DemoQuery, Now: demoNow,
		Keys: []string{"enter", "T"}, Caption: "Session-Detail mit Treffer, Tool-Aufrufe eingeblendet"},
	{Name: "stats", Width: Width, Height: Height, Start: tui.ViewStats, Now: weekNow,
		Caption: "Statistik: gestapeltes Chart je Tag und Projekt mit Tabelle"},
	{Name: "heatmap", Width: Width, Height: Height, Start: tui.ViewStats, Now: weekNow, Keys: []string{"h"},
		Caption: "Heatmap Wochentag × Stunde unter dem Chart"},
	{Name: "activity-week", Width: Width, Height: Height, Start: tui.ViewActivity, Now: weekNow,
		Caption: "Aktivität je Tag, eine Woche mit Kernzeit, Feiertag und Kennzahlen"},
	{Name: "activity-month", Width: Width, Height: Height, Start: tui.ViewActivity, Now: monthNow, Keys: []string{"m"},
		Caption: "Aktivität je Tag, ein Monat mit Monatsarbeitszeit"},
	{Name: "compact", Width: CompactWidth, Height: CompactHeight, Start: tui.ViewActivity, Now: weekNow,
		Caption: "Kompakte Ansicht bei 80×24"},
}

// Archives are the two stores the shots read from. Either may be nil, then
// the shots that need it fail.
type Archives struct {
	Anon, Demo *store.Store
}

// workHours mirrors the defaults of the [work] config table.
var workHours = store.WorkHours{
	Start: 9 * time.Hour, End: 17 * time.Hour, Target: 8 * time.Hour,
	Low: 6 * time.Hour, High: 9 * time.Hour, WeeklyLow: 32 * time.Hour, WeeklyHigh: 48 * time.Hour,
}

// options builds the app options of a shot: the config defaults (time zone
// Europe/Berlin, day start 04:00, working time, holidays of
// Baden-Württemberg, own messages only), text charts and the shot's clock.
func options(s Shot, a Archives) (tui.Options, error) {
	st := a.Anon
	if s.Demo {
		st = a.Demo
	}
	if st == nil {
		return tui.Options{}, fmt.Errorf("shot %s: no archive", s.Name)
	}
	now := s.Now
	home := "/w"
	if s.Demo {
		home = "/home/mara"
	}
	return tui.Options{
		Store: st, Location: berlin, Now: func() time.Time { return now },
		Start: s.Start, Query: s.Query, Graphics: tui.GraphicsText, NoMouse: true,
		DayStart: 4 * time.Hour, Work: workHours, Holidays: holiday.New("Baden-Württemberg"), HumanOnly: true, Home: home,
	}, nil
}

// Render drives the app of a shot headlessly and returns its final frame.
// Commands run synchronously as they are returned, so every load has
// landed when the frame is taken.
func Render(s Shot, a Archives) (Frame, error) {
	opts, err := options(s, a)
	if err != nil {
		return Frame{}, err
	}
	app, err := tui.New(opts)
	if err != nil {
		return Frame{}, fmt.Errorf("shot %s: %w", s.Name, err)
	}
	var m tea.Model = app
	m = send(m, tea.WindowSizeMsg{Width: s.Width, Height: s.Height})
	m = run(m, m.Init())
	for _, k := range s.Keys {
		m = send(m, keyPress(k))
	}
	return Frame{Shot: s, Content: m.View().Content}, nil
}

// RenderAll renders every one of Shots.
func RenderAll(a Archives) ([]Frame, error) {
	out := make([]Frame, 0, len(Shots))
	for _, s := range Shots {
		f, err := Render(s, a)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// keyPress builds the key press for a name: a single character or one of
// enter, tab, esc.
func keyPress(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	r := []rune(name)
	if len(r) != 1 {
		panic("screenshot: unknown key " + name)
	}
	return tea.KeyPressMsg{Code: r[0], Text: name}
}

// send hands msg to m and runs the returned command to completion.
func send(m tea.Model, msg tea.Msg) tea.Model {
	m, cmd := m.Update(msg)
	return run(m, cmd)
}

// run executes cmd synchronously against m: batches and sequences are
// unpacked in order, raw terminal writes are dropped, every other message
// goes to m.Update and the command it returns runs in turn. It returns the
// model after the last message.
func run(m tea.Model, cmd tea.Cmd) tea.Model {
	exec(func(msg tea.Msg) tea.Cmd {
		var next tea.Cmd
		m, next = m.Update(msg)
		return next
	}, cmd)
	return m
}

func exec(update func(tea.Msg) tea.Cmd, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	switch v := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, c := range v {
			exec(update, c)
		}
		return
	case tea.RawMsg, tea.QuitMsg:
		return
	}
	// tea.Sequence yields an unexported slice of commands.
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
		for i := range v.Len() {
			if c, ok := v.Index(i).Interface().(tea.Cmd); ok {
				exec(update, c)
			}
		}
		return
	}
	exec(update, update(msg))
}
