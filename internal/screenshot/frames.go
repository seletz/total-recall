package screenshot

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/holiday"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui"
)

// Sizes of the screenshots in terminal cells.
const (
	Width, Height               = 120, 36
	CompactWidth, CompactHeight = 80, 24
)

// Shot describes one screenshot: the view it starts in, the clock the view
// sees and the keys pressed before the frame is captured. The clock pins
// the windows of the stats and activity views to fixed dates, so a rerun
// yields the same frame as long as the archive does not change.
type Shot struct {
	Name          string
	Width, Height int
	Start         string
	Query         string
	Now           time.Time
	// Keys are pressed in order: single characters, "enter", "tab", "esc".
	Keys []string
	// Outdated renders against the home with the outdated archive
	// (Env.Outdated) instead of the demo archive.
	Outdated bool
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

// Clocks of the shots, all in the demo archive's past. The search shots use
// the archive's own clock, the morning after its last conversation. The
// stats window is the 28 days up to 2026-11-01, all of October. The
// activity shots show the hand-laid week of 2026-06-01 with Fronleichnam on
// the Thursday, that week moved three days back by ↑, and June 2026 as the
// month.
var (
	searchNow = demo.Now
	statsNow  = time.Date(2026, 11, 1, 12, 0, 0, 0, berlin)
	weekNow   = time.Date(2026, 6, 7, 12, 0, 0, 0, berlin)
	monthNow  = time.Date(2026, 6, 30, 12, 0, 0, 0, berlin)
)

// Shots are the screenshots of the docs, in the order of docs/reference/30-tui.md.
var Shots = []Shot{
	{Name: "search", Width: Width, Height: Height, Start: tui.ViewSearch, Query: demo.Query, Now: searchNow,
		Caption: "Suche mit Vorschau"},
	{Name: "session", Width: Width, Height: Height, Start: tui.ViewSearch, Query: demo.Query, Now: searchNow,
		Keys: []string{"enter", "T"}, Caption: "Session-Detail mit Treffer, Tool-Aufrufe eingeblendet"},
	{Name: "stats", Width: Width, Height: Height, Start: tui.ViewStats, Now: statsNow,
		Caption: "Statistik: gestapeltes Chart je Tag und Projekt mit Tabelle"},
	{Name: "heatmap", Width: Width, Height: Height, Start: tui.ViewStats, Now: statsNow, Keys: []string{"h"},
		Caption: "Heatmap Wochentag × Stunde unter dem Chart"},
	{Name: "activity-week", Width: Width, Height: Height, Start: tui.ViewActivity, Now: weekNow,
		Caption: "Aktivität je Tag, eine Woche mit Kernzeit, Feiertag und Kennzahlen"},
	{Name: "activity-month", Width: Width, Height: Height, Start: tui.ViewActivity, Now: monthNow, Keys: []string{"m"},
		Caption: "Aktivität je Tag, ein Monat mit Monatsarbeitszeit"},
	{Name: "activity-days", Width: Width, Height: Height, Start: tui.ViewActivity, Now: weekNow, Keys: []string{"k", "k", "k"},
		Caption: "Aktivität je Tag, mit ↑ drei Tage zurück: ein gleitendes Fenster von sieben Tagen"},
	{Name: "compact", Width: CompactWidth, Height: CompactHeight, Start: tui.ViewActivity, Now: weekNow,
		Caption: "Kompakte Ansicht bei 80×24"},
	{Name: "help", Width: Width, Height: Height, Start: tui.ViewStats, Now: statsNow, Keys: []string{"?"},
		Caption: "Hilfe-Overlay mit den Tasten der Statistik und den globalen Tasten"},
	{Name: "heatmap-compact", Width: CompactWidth, Height: CompactHeight, Start: tui.ViewStats, Now: statsNow, Keys: []string{"h"},
		Caption: "Statistik mit Heatmap bei 80×24: kompaktes Layout, Tabelle ausgeblendet"},
	{Name: "config-status", Width: Width, Height: Height, Start: tui.ViewConfig, Now: searchNow,
		Caption: "Config, Seite Status: Archiv, Schema, Zähler, letztes Backup, Fehler des Logs"},
	{Name: "config-settings", Width: Width, Height: Height, Start: tui.ViewConfig, Now: searchNow, Keys: []string{"tab", "j", "j", "j", "j", "enter"},
		Caption: "Config, Seite Settings: das Backup-Verzeichnis wird in der Eingabezeile bearbeitet"},
	{Name: "config-actions", Width: Width, Height: Height, Start: tui.ViewConfig, Now: searchNow, Keys: []string{"shift+tab", "shift+tab", "enter"},
		Caption: "Config, Seite Actions: Backup wartet auf die Bestätigung"},
	{Name: "config-projects", Width: Width, Height: Height, Start: tui.ViewConfig, Now: searchNow, Keys: []string{"tab", "tab"},
		Caption: "Config, Seite Projects: die Projektdefinitionen der Datei mit Farbe, Beschreibung und Mustern"},
	{Name: "config-migrate", Width: Width, Height: Height, Start: tui.ViewConfig, Now: searchNow, Keys: []string{"shift+tab", "shift+tab", "j", "j", "j", "enter"}, Outdated: true,
		Caption: "Config, Seite Actions bei anstehender Migration: der Plan wartet auf die Bestätigung"},
	{Name: "config-setup", Width: Width, Height: Height, Start: tui.ViewConfig, Now: searchNow, Keys: []string{"shift+tab"},
		Caption: "Config, Seite Setup: Prüfungen der Installation und der Hook-Block"},
}

// workHours mirrors the defaults of the [work] config table.
var workHours = store.WorkHours{
	Start: 9 * time.Hour, End: 17 * time.Hour, Target: 8 * time.Hour,
	Low: 6 * time.Hour, High: 9 * time.Hour, WeeklyLow: 32 * time.Hour, WeeklyHigh: 48 * time.Hour,
}

// options builds the app options of a shot: the config defaults (time zone
// Europe/Berlin, day start 04:00, working time, holidays of
// Baden-Württemberg, own messages only), the synthetic home directory, text
// charts and the shot's clock.
func options(s Shot, env *Env) (tui.Options, error) {
	if s.Outdated && env != nil {
		env = env.Outdated
	}
	if env == nil || env.Store == nil {
		return tui.Options{}, fmt.Errorf("shot %s: no archive", s.Name)
	}
	now := s.Now
	cv := env.View
	cv.Now = func() time.Time { return now }
	return tui.Options{
		Store: env.Store, Location: berlin, Now: func() time.Time { return now },
		Start: s.Start, Query: s.Query, Graphics: tui.GraphicsText, NoMouse: true,
		DayStart: 4 * time.Hour, Work: workHours, Holidays: holiday.New("Baden-Württemberg"), HumanOnly: true, Home: env.Home,
		Config: env.Config, ConfigView: cv,
		// A shot never waits for a reload tick: the renderer runs commands
		// synchronously, and a tick would block it.
		Tick: func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil },
	}, nil
}

// Render drives the app of a shot headlessly against the archive st (the
// demo archive, see package demo) and returns its final frame. Commands
// run synchronously as they are returned, so every load has landed when
// the frame is taken.
func Render(s Shot, env *Env) (Frame, error) {
	opts, err := options(s, env)
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
func RenderAll(env *Env) ([]Frame, error) {
	out := make([]Frame, 0, len(Shots))
	for _, s := range Shots {
		f, err := Render(s, env)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// keyPress builds the key press for a name: a single character or one of
// enter, tab, shift+tab, esc.
func keyPress(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
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
