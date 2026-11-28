// Package shots renders the screenshots of the TUI of
// total-recall-analytics for the docs: every Shot drives the view
// headlessly (keys and clicks) against an analytics database built from
// the demo pool, and the final frame becomes a PNG (the renderer of
// total-recall, internal/screenshot) and a text golden.
package shots

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/analytics/internal/duck"
	"github.com/seletz/total-recall/analytics/internal/report"
	"github.com/seletz/total-recall/analytics/internal/star"
	"github.com/seletz/total-recall/analytics/internal/tui"
	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/screenshot"
)

// Size of every shot, as the shots of total-recall.
const (
	Width  = 120
	Height = 36
)

// Shot is one screenshot: the starting view, the keys and clicks after it
// and the caption for the docs. A key is a key name ("m", "left", "?");
// "click:LABEL=TEXT" clicks on TEXT in the toggle row LABEL (show, by,
// period, host, range), "click:TEXT" on the first TEXT of the frame,
// "wheel:up" and "wheel:down" turn the mouse wheel.
type Shot struct {
	Name    string
	Keys    []string
	Caption string
}

// Shots are the screenshots of the docs, one per view and state.
var Shots = []Shot{
	{Name: "analytics-usage", Caption: "Analyse-TUI: Events je Projekt und Woche über alle Hosts des Pools"},
	{Name: "analytics-host", Keys: []string{"click:host=host-b"},
		Caption: "Analyse-TUI: dieselbe Auswertung nur für host-b, per Klick in der Zeile host"},
	{Name: "analytics-by-host", Keys: []string{"click:by=host"},
		Caption: "Analyse-TUI: Events je Host und Woche, per Klick in der Zeile by"},
	{Name: "analytics-tokens", Keys: []string{"click:show=tokens", "click:by=model", "click:period=month"},
		Caption: "Analyse-TUI: Tokens je Modell und Monat; der Modellwechsel im August ist zu sehen"},
	{Name: "analytics-hours", Keys: []string{"click:show=hours", "click:by=category"},
		Caption: "Analyse-TUI: Arbeitszeit je Kategorie und Woche über alle Hosts"},
	{Name: "analytics-days", Keys: []string{"click:period=day", "click:range=←", "click:range=←"},
		Caption: "Analyse-TUI: Tage, mit dem Pfeil in der Zeile range zwei halbe Fenster zurück"},
	{Name: "analytics-help", Keys: []string{"?"},
		Caption: "Analyse-TUI: die Hilfe mit allen Tasten"},
}

// Demo builds the analytics database of the demo pool below dir, as
// `build --pool` does (one source per host, its archive ID), and returns
// its path and the configuration it used.
func Demo(ctx context.Context, dir string) (string, *config.Config, error) {
	p, err := demo.BuildPool(ctx, filepath.Join(dir, "pool"))
	if err != nil {
		return "", nil, err
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("timezone = \"Europe/Berlin\"\n"+demo.AccountsTOML), 0o644); err != nil {
		return "", nil, err
	}
	cfg, err := config.Load(config.Options{ConfigFile: cfgPath, GOOS: "linux", Home: demo.Home,
		Getenv: func(string) string { return "" }, Hostname: func() (string, error) { return demo.Host, nil }})
	if err != nil {
		return "", nil, err
	}
	projects, err := cfg.ProjectResolver()
	if err != nil {
		return "", nil, err
	}
	accounts, _, err := cfg.AccountResolver()
	if err != nil {
		return "", nil, err
	}
	ws, err := cfg.WorktimeSettings()
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, "analytics.duckdb")
	db, err := duck.Open(ctx, duck.Options{Path: path})
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = db.Close() }()
	for _, h := range p.Hosts {
		_, err := star.Run(ctx, db, star.Options{
			Archive: h.Path, Source: h.Host, Host: h.Host, ArchiveID: demo.PoolArchiveID(h.Host),
			TZ: cfg.Location(), Home: demo.Home, Projects: projects,
			Now:      func() time.Time { return demo.Now },
			Worktime: &star.Worktime{Settings: ws, DayStart: cfg.DayStartOffset()},
			Accounts: func(id, org string) (string, string) {
				i := accounts.Resolve(id, org)
				return i.Name, i.Group
			},
		})
		if err != nil {
			return "", nil, fmt.Errorf("shots: %s: %w", h.Host, err)
		}
	}
	return path, cfg, nil
}

// Loader opens the database at path read-only for every load, as the tui
// command does.
func Loader(path string) tui.Loader {
	return func(ctx context.Context, run func(report.Querier) error) error {
		db, err := duck.Open(ctx, duck.Options{Path: path, ReadOnly: true})
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		return run(db)
	}
}

// Render drives the view of s against the database at path and returns
// its final frame. The header names the database where the tui command
// would find it on the demo machine.
func Render(s Shot, path string) (screenshot.Frame, error) {
	m := tea.Model(tui.New(tui.Options{Load: Loader(path), Home: demo.Home, TZ: "Europe/Berlin", NoMouse: true,
		Path: demo.Home + "/.local/share/total-recall/analytics.duckdb"}))
	m = run(m, m.Init())
	m = send(m, tea.WindowSizeMsg{Width: Width, Height: Height})
	for _, k := range s.Keys {
		if dir, ok := strings.CutPrefix(k, "wheel:"); ok {
			button := tea.MouseWheelDown
			if dir == "up" {
				button = tea.MouseWheelUp
			}
			m = send(m, tea.MouseWheelMsg{X: Width / 2, Y: Height / 2, Button: button})
			continue
		}
		if target, ok := strings.CutPrefix(k, "click:"); ok {
			click, err := clickOn(m.View().Content, target)
			if err != nil {
				return screenshot.Frame{}, fmt.Errorf("shot %s: %w", s.Name, err)
			}
			m = send(m, click)
			continue
		}
		m = send(m, keyPress(k))
	}
	return screenshot.Frame{Shot: screenshot.Shot{Name: s.Name, Width: Width, Height: Height, Caption: s.Caption}, Content: m.View().Content}, nil
}

// send hands msg to m and runs what it returns.
func send(m tea.Model, msg tea.Msg) tea.Model {
	m, cmd := m.Update(msg)
	return run(m, cmd)
}

// run executes cmd and every command that follows, synchronously.
func run(m tea.Model, cmd tea.Cmd) tea.Model {
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case nil, tea.QuitMsg:
		return m
	case tea.BatchMsg:
		for _, c := range msg {
			m = run(m, c)
		}
		return m
	default:
		return send(m, msg)
	}
}

// keyPress builds the press of a key name.
func keyPress(name string) tea.KeyPressMsg {
	switch name {
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	r := []rune(name)
	return tea.KeyPressMsg{Code: r[0], Text: name}
}

// clickOn builds the click on target in frame: "LABEL=TEXT" is TEXT in the
// row that starts with LABEL, anything else the first occurrence.
func clickOn(frame, target string) (tea.MouseClickMsg, error) {
	label, text, inRow := strings.Cut(target, "=")
	if !inRow {
		text = target
	}
	for y, line := range strings.Split(ansi.Strip(frame), "\n") {
		from := 0
		if inRow {
			if !strings.HasPrefix(strings.TrimLeft(line, " "), label+" ") {
				continue
			}
			from = len(" ") + len(label)
		}
		if x := strings.Index(line[from:], text); x >= 0 {
			// A toggle reads as a word: skip a match inside a longer one.
			x += from
			return tea.MouseClickMsg{X: ansi.StringWidth(line[:x]), Y: y, Button: tea.MouseLeft}, nil
		}
	}
	return tea.MouseClickMsg{}, fmt.Errorf("click: %q is not in the frame", target)
}
