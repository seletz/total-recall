// Package tui is the interactive terminal UI of total-recall: an app
// shell that hosts the search, stats and activity views, switches between
// them, shows a help overlay built from their key maps and keeps the views
// current while the hooks write to the archive.
//
// Views live in their own packages (search, stats, …) and implement [View]
// without importing this package. The app forwards key presses to the active
// view and every other message to all views; the active view may claim the
// keyboard with [Focused]. Mouse clicks and wheel turns in the body reach
// the current view with coordinates relative to the body; a click on a tab
// in the header switches the view, a click on a key hint in the footer
// presses that key, a click beside the help panel closes the help. The
// session detail is a layer above the views:
// opened by the search view's OpenSession, closed by its Back, it replaces
// the body while the search view keeps its state underneath. The memory
// view (package memview) is a second such layer, opened by a memory hit,
// and the attachment view (package attview) a third, opened from an event
// with images or documents in the search or in the session. The day view
// (package dayview) is a fourth, opened from the activity view by a click
// or enter on a day; a session opened from it returns to it.
//
// Reloading: every Options.Reload the app asks the store whether anything
// changed (store.Poll, one PRAGMA, no queries otherwise). When it did, the
// view on screen reloads in place (View.Reload keeps mark, scroll, focus
// and input) and the others are marked stale, so they reload when they
// are shown next. A swapped file (maintenance) is reopened first. The
// header shows when the data was last loaded; the reload key does it on
// demand.
package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/dayreport"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/activity"
	"github.com/seletz/total-recall/internal/tui/attview"
	"github.com/seletz/total-recall/internal/tui/chart"
	"github.com/seletz/total-recall/internal/tui/configview"
	"github.com/seletz/total-recall/internal/tui/dayview"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/tui/memview"
	"github.com/seletz/total-recall/internal/tui/search"
	"github.com/seletz/total-recall/internal/tui/session"
	"github.com/seletz/total-recall/internal/tui/stats"
	"github.com/seletz/total-recall/internal/tui/theme"
	"github.com/seletz/total-recall/internal/worktime"
)

// Graphics selects how charts are drawn; the stats view owns the type.
type Graphics = stats.Graphics

// Graphics modes. Auto probes the terminal for Kitty graphics, Text never
// does, Kitty forces images without probing.
const (
	GraphicsAuto  = stats.GraphicsAuto
	GraphicsText  = stats.GraphicsText
	GraphicsKitty = stats.GraphicsKitty
)

// ParseGraphics validates a --graphics flag value; empty means auto.
func ParseGraphics(s string) (Graphics, error) {
	switch g := Graphics(s); g {
	case "":
		return GraphicsAuto, nil
	case GraphicsAuto, GraphicsText, GraphicsKitty:
		return g, nil
	default:
		return "", fmt.Errorf("invalid graphics mode %q (use auto, text or kitty)", s)
	}
}

// View names, the argument of `total-recall tui [view]`.
const (
	ViewSearch   = "search"
	ViewStats    = "stats"
	ViewActivity = "activity"
	ViewConfig   = "config"
)

// ViewNames lists the views in tab order.
var ViewNames = []string{ViewSearch, ViewStats, ViewActivity, ViewConfig}

// Options configure the app.
type Options struct {
	// Store is the read-only archive; nil in shell tests.
	Store *store.Store
	// Config is the effective configuration the config view shows and
	// edits; nil in shell tests (the view shows a hint).
	Config *config.Config
	// ConfigView are the further options of the config view: version of
	// the binary, settings file of the harness, probes. Config, Store,
	// Location, Now, Keys and Home are filled in from this Options.
	ConfigView configview.Options
	// Location is the time zone for day boundaries; nil means UTC.
	Location *time.Location
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Start is the view shown first (one of ViewNames); empty means search.
	Start string
	// Query is the initial search text.
	Query string
	// Graphics is the chart mode; empty means auto.
	Graphics Graphics
	// DayStart is the day boundary of the activity view as an offset from
	// local midnight (4h = 04:00); zero means midnight.
	DayStart time.Duration
	// Work is the working time of the activity view; the zero value shows
	// neither core hours nor a target.
	Work store.WorkHours
	// Holidays names the public holidays of the activity and stats views;
	// nil means there are none.
	Holidays store.Holidays
	// HumanOnly makes the activity view start counting own messages only.
	HumanOnly bool
	// Worktime are the settings of the working-time estimate ([worktime])
	// and BlockGap the gap that ends an activity block ([work] block_gap),
	// both for the day view; without them it shows a hint.
	Worktime worktime.Settings
	BlockGap time.Duration
	// Home is the home directory, shown as "~" in project names of
	// directories without a repository; empty keeps full paths.
	Home string
	// Projects assigns sessions to the configured projects ([[project]])
	// and supplies their colours; the zero value has none.
	Projects project.Resolver
	// NoMouse leaves the mouse to the terminal: no wheel scrolling and no
	// clicks, but text is selected by plain dragging.
	NoMouse bool
	// StatsPeriod and StatsDimension are what the stats view starts with
	// (day, week, month; project, agent, directory); empty means days by
	// project.
	StatsPeriod    string
	StatsDimension string
	// ActivityWindow is the window the activity view starts with (day,
	// week, month); empty means week.
	ActivityWindow string
	// ActivityProjects starts the activity view with the bars coloured
	// by project.
	ActivityProjects bool
	// ActivityNewest is where the activity view puts the newest day
	// (bottom, top); empty means bottom.
	ActivityNewest string
	// Keys are the effective key bindings; nil means the defaults.
	Keys keys.Map
	// Reload is how often the archive is checked for changes; 0 never
	// checks (the reload key still works).
	Reload time.Duration
	// Tick schedules the next check after d; nil means tea.Tick. Tests
	// pass one that fires on demand.
	Tick func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd
}

func (o *Options) defaults() {
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Start == "" {
		o.Start = ViewSearch
	}
	if o.Graphics == "" {
		o.Graphics = GraphicsAuto
	}
	if o.StatsPeriod == "" {
		o.StatsPeriod = "day"
	}
	if o.StatsDimension == "" {
		o.StatsDimension = "project"
	}
	if o.ActivityWindow == "" {
		o.ActivityWindow = "week"
	}
	if o.Tick == nil {
		o.Tick = tea.Tick
	}
}

// View is one screen of the app. Its View is rendered into the body between
// header and footer; its key bindings feed the footer and the help overlay.
type View interface {
	tea.Model
	help.KeyMap
	// Title names the view in the header, e.g. "search".
	Title() string
	// Reload loads the view's data again in place: the same query, window
	// or session, keeping mark, scroll position, focus and input. Views
	// without a database do nothing.
	Reload() (tea.Model, tea.Cmd)
}

// Focused is implemented by views that take text input. While it reports
// true the app passes every key except ctrl+c to the view, so typing "q" or
// "1" neither quits nor switches views.
type Focused interface {
	Focused() bool
}

// New builds the app for opts.
func New(opts Options) (App, error) {
	opts.defaults()
	// One colour assignment for the stats, the activity and the config
	// view, so a project looks alike in all of them.
	colors := &chart.Colors{}
	so, err := statsOptions(opts, colors)
	if err != nil {
		return App{}, err
	}
	ao, err := activityOptions(opts, colors)
	if err != nil {
		return App{}, err
	}
	views := []View{search.New(searchOptions(opts)), stats.New(so), activity.New(ao), configview.New(configOptions(opts, colors))}
	active := -1
	for i, v := range views {
		if v.Title() == opts.Start {
			active = i
		}
	}
	if active < 0 {
		return App{}, fmt.Errorf("unknown view %q (use %s)", opts.Start, strings.Join(ViewNames, ", "))
	}
	detail := session.New(opts.Store, opts.Location, opts.Keys)
	var memLoader memview.Loader
	if opts.Store != nil {
		memLoader = opts.Store
	}
	var attLoader attview.Loader
	if opts.Store != nil {
		attLoader = opts.Store
	}
	attach := attview.New(attview.Options{Loader: attLoader, Location: opts.Location, External: &attview.SystemViewer{}, Keys: opts.Keys})
	a := App{opts: opts, views: views, active: active, detail: detail, memory: memview.New(memLoader, opts.Location, opts.Keys), attach: attach,
		keys: newKeyMap(opts.Keys), th: theme.New(true), stale: make([]bool, len(views)+4), updated: opts.Now(),
		day: dayview.New(dayOptions(opts, colors)), colors: colors}
	if opts.Store != nil {
		// The baseline of the change check is taken before the views load
		// for the first time, so nothing written in between is missed.
		_, _ = opts.Store.Poll(context.Background())
	}
	return a, nil
}

// Run starts the app on in/out and blocks until it quits.
func Run(ctx context.Context, opts Options, in io.Reader, out io.Writer) error {
	app, err := New(opts)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(app, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}

// configOptions is what the config view gets from the app options.
func configOptions(opts Options, colors *chart.Colors) configview.Options {
	co := opts.ConfigView
	co.Config, co.Location, co.Now, co.Keys, co.Home, co.Colors = opts.Config, opts.Location, opts.Now, opts.Keys, opts.Home, colors
	if opts.Store != nil {
		co.Store = opts.Store
	}
	return co
}

// searchOptions is what the search view gets from the app options. A nil
// store stays a nil Searcher and Loader, so the view can tell there is no
// database.
func searchOptions(opts Options) search.Options {
	so := search.Options{Query: opts.Query, Now: opts.Now, Location: opts.Location, Keys: opts.Keys, Resolver: opts.Projects, Home: opts.Home}
	if opts.Store != nil {
		so.Searcher, so.Loader, so.Projects = opts.Store, opts.Store, opts.Store
	}
	return so
}

// statsOptions is what the stats view gets from the app options; a nil store
// stays a nil Loader. The period and dimension names are parsed here.
func statsOptions(opts Options, colors *chart.Colors) (stats.Options, error) {
	p, err := store.ParsePeriod(opts.StatsPeriod)
	if err != nil {
		return stats.Options{}, fmt.Errorf("stats period: %w", err)
	}
	d, err := store.ParseDimension(opts.StatsDimension)
	if err != nil {
		return stats.Options{}, fmt.Errorf("stats dimension: %w", err)
	}
	so := stats.Options{Now: opts.Now, Location: opts.Location, Graphics: opts.Graphics, Home: opts.Home, Period: p, Dimension: d, Keys: opts.Keys, Holidays: opts.Holidays, Projects: opts.Projects, Colors: colors}
	if opts.Store != nil {
		so.Loader = opts.Store
	}
	return so, nil
}

// activityOptions is what the activity view gets from the app options; a
// nil store stays a nil Loader. The window name is parsed here.
func activityOptions(opts Options, colors *chart.Colors) (activity.Options, error) {
	if _, err := store.ParsePeriod(opts.ActivityWindow); err != nil {
		return activity.Options{}, fmt.Errorf("activity window: %w", err)
	}
	ao := activity.Options{Now: opts.Now, Location: opts.Location, DayStart: opts.DayStart, Work: opts.Work, Window: opts.ActivityWindow, Keys: opts.Keys, Holidays: opts.Holidays, HumanOnly: opts.HumanOnly, ByProject: opts.ActivityProjects, Newest: opts.ActivityNewest, Home: opts.Home, Projects: opts.Projects, Colors: colors}
	if opts.Store != nil {
		ao.Loader = opts.Store
	}
	return ao, nil
}

// dayOptions is what the day view gets from the app options; a nil store
// stays a nil Source.
func dayOptions(opts Options, colors *chart.Colors) dayview.Options {
	do := dayview.Options{
		Now: opts.Now, Keys: opts.Keys, Colors: colors,
		Report: dayreport.Options{
			Location: opts.Location, DayStart: opts.DayStart, BlockGap: opts.BlockGap, HumanOnly: opts.HumanOnly, Work: opts.Work, Holidays: opts.Holidays,
			Worktime: opts.Worktime, Projects: opts.Projects, Home: opts.Home,
		},
	}
	if opts.Store != nil {
		do.Source = opts.Store
	}
	return do
}
