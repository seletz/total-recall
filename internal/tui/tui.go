// Package tui is the interactive terminal UI of total-recall (spike): an app
// shell that hosts the search, stats and activity views, switches between
// them and shows a help overlay built from their key maps.
//
// Views live in their own packages (search, stats, …) and implement [View]
// without importing this package. The app forwards key presses to the active
// view and every other message to all views; the active view may claim the
// keyboard with [Focused]. Mouse clicks and wheel turns in the body reach
// the current view with coordinates relative to the body; clicks on the
// header switch the view. The session detail is a layer above the views:
// opened by the search view's OpenSession, closed by its Back, it replaces
// the body while the search view keeps its state underneath.
package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/activity"
	"github.com/seletz/total-recall/internal/tui/search"
	"github.com/seletz/total-recall/internal/tui/session"
	"github.com/seletz/total-recall/internal/tui/stats"
	"github.com/seletz/total-recall/internal/tui/theme"
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
)

// ViewNames lists the views in tab order.
var ViewNames = []string{ViewSearch, ViewStats, ViewActivity}

// Options configure the app.
type Options struct {
	// Store is the read-only archive; nil in shell tests.
	Store *store.Store
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
	// Home is the home directory, shown as "~" in project names of
	// directories without a repository; empty keeps full paths.
	Home string
	// NoMouse leaves the mouse to the terminal: no wheel scrolling and no
	// clicks, but text is selected by plain dragging.
	NoMouse bool
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
}

// View is one screen of the app. Its View is rendered into the body between
// header and footer; its key bindings feed the footer and the help overlay.
type View interface {
	tea.Model
	help.KeyMap
	// Title names the view in the header, e.g. "search".
	Title() string
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
	views := []View{search.New(searchOptions(opts)), stats.New(statsOptions(opts)), activity.New(activityOptions(opts))}
	active := -1
	for i, v := range views {
		if v.Title() == opts.Start {
			active = i
		}
	}
	if active < 0 {
		return App{}, fmt.Errorf("unknown view %q (use %s)", opts.Start, strings.Join(ViewNames, ", "))
	}
	detail := session.New(opts.Store, opts.Location)
	return App{opts: opts, views: views, active: active, detail: detail, keys: defaultKeyMap(), th: theme.New(true)}, nil
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

// searchOptions is what the search view gets from the app options. A nil
// store stays a nil Searcher and Loader, so the view can tell there is no
// database.
func searchOptions(opts Options) search.Options {
	so := search.Options{Query: opts.Query, Now: opts.Now, Location: opts.Location}
	if opts.Store != nil {
		so.Searcher, so.Loader = opts.Store, opts.Store
	}
	return so
}

// statsOptions is what the stats view gets from the app options; a nil store
// stays a nil Loader.
func statsOptions(opts Options) stats.Options {
	so := stats.Options{Now: opts.Now, Location: opts.Location, Graphics: opts.Graphics, Home: opts.Home, Holidays: opts.Holidays}
	if opts.Store != nil {
		so.Loader = opts.Store
	}
	return so
}

// activityOptions is what the activity view gets from the app options; a
// nil store stays a nil Loader.
func activityOptions(opts Options) activity.Options {
	ao := activity.Options{Now: opts.Now, Location: opts.Location, DayStart: opts.DayStart, Work: opts.Work, Holidays: opts.Holidays, HumanOnly: opts.HumanOnly}
	if opts.Store != nil {
		ao.Loader = opts.Store
	}
	return ao
}
