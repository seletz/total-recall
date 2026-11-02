package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/tui"
)

// tuiFlags are the command-line overrides of the [tui] config table. The
// Set fields tell a flag given on the command line from its default, so
// that --no-mouse=false overrides mouse = false in the config.
type tuiFlags struct {
	View        string
	Query       string
	Graphics    string
	NoMouse     bool
	NoMouseSet  bool
	GraphicsSet bool
}

func (e *env) tuiCmd() *cobra.Command {
	var f tuiFlags
	c := &cobra.Command{
		Use:   "tui [search|stats|activity]",
		Short: "Interactive search, activity charts and first/last activity per day",
		Long: `tui opens the archive read-only in a full-screen terminal UI with a live
search view, an activity statistics view and a view of the first and the
last activity per day (see stats --workday). The optional argument selects
the view shown first.

Defaults for the view, the chart mode, the mouse and the start-up state of
the stats and activity views come from the [tui] table of the config;
flags and the argument override them. Key bindings come from [keys].`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				f.View = args[0]
			}
			f.GraphicsSet, f.NoMouseSet = cmd.Flags().Changed("graphics"), cmd.Flags().Changed("no-mouse")
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			opts, err := tuiOptions(cfg, f)
			if err != nil {
				return err
			}
			st, err := e.openRead(cfg)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			opts.Store, opts.Now = st, e.app.Now
			return tui.Run(cmd.Context(), opts, e.app.Stdin, e.app.Stdout)
		},
	}
	c.Flags().StringVar(&f.Query, "query", "", "initial search text")
	c.Flags().StringVar(&f.Graphics, "graphics", "", "chart rendering: auto, text or kitty (default from config: auto)")
	c.Flags().BoolVar(&f.NoMouse, "no-mouse", false, "leave the mouse to the terminal (plain drag selects text; default from config)")
	return c
}

// tuiOptions builds the app options from the config and the flags; a flag
// that was given wins over the config, which wins over the built-in
// default. The store and the clock are added by the caller.
func tuiOptions(cfg *config.Config, f tuiFlags) (tui.Options, error) {
	if err := cfg.ValidateTUI(); err != nil {
		return tui.Options{}, err
	}
	km, err := cfg.KeyMap()
	if err != nil {
		return tui.Options{}, err
	}
	work, err := workHours(cfg)
	if err != nil {
		return tui.Options{}, err
	}
	view := cfg.TUI.View
	if f.View != "" {
		view = f.View
	}
	if !slices.Contains(tui.ViewNames, view) {
		return tui.Options{}, fmt.Errorf("unknown view %q (use %s)", view, strings.Join(tui.ViewNames, ", "))
	}
	graphics := cfg.TUI.Graphics
	if f.GraphicsSet {
		graphics = f.Graphics
	}
	g, err := tui.ParseGraphics(graphics)
	if err != nil {
		return tui.Options{}, err
	}
	noMouse := !cfg.TUI.Mouse
	if f.NoMouseSet {
		noMouse = f.NoMouse
	}
	return tui.Options{
		Location: cfg.Location(), Start: view, Query: f.Query, Graphics: g, NoMouse: noMouse,
		DayStart: cfg.DayStartOffset(), Work: work, Holidays: holidays(cfg), HumanOnly: cfg.Work.HumanOnly(), Home: cfg.Home,
		StatsPeriod: cfg.TUI.StatsPeriod, StatsDimension: cfg.TUI.StatsDimension, ActivityWindow: cfg.TUI.ActivityWindow,
		Keys: km,
	}, nil
}
