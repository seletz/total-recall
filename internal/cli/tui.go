package cli

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/tui"
	"github.com/seletz/total-recall/internal/tui/configview"
	"github.com/seletz/total-recall/internal/version"
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
	Reload      time.Duration
	ReloadSet   bool
}

func (e *env) tuiCmd() *cobra.Command {
	var f tuiFlags
	c := &cobra.Command{
		Use:   "tui [search|stats|activity|config]",
		Short: "Interactive search, activity charts and first/last activity per day",
		Long: `tui opens the archive read-only in a full-screen terminal UI with a live
search view, an activity statistics view and a view of the first and the
last activity per day (see stats --workday). The optional argument selects
the view shown first.

Defaults for the view, the chart mode, the mouse, the reload interval and
the start-up state of the stats and activity views come from the [tui]
table of the config; flags and the argument override them. Key bindings
come from [keys].

The views reload by themselves while the hooks write to the archive: every
--reload interval the TUI checks whether the archive changed and, only
then, loads the view on screen again, keeping mark, scroll position and
input. --reload 0 switches that off; R reloads on demand either way.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				f.View = args[0]
			}
			f.GraphicsSet, f.NoMouseSet, f.ReloadSet = cmd.Flags().Changed("graphics"), cmd.Flags().Changed("no-mouse"), cmd.Flags().Changed("reload")
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
			opts.Config = cfg
			opts.ConfigView = configview.Options{Version: version.Version, Getenv: e.app.Getenv}
			return tui.Run(cmd.Context(), opts, e.app.Stdin, e.app.Stdout)
		},
	}
	c.Flags().StringVar(&f.Query, "query", "", "initial search text")
	c.Flags().StringVar(&f.Graphics, "graphics", "", "chart rendering: auto, text or kitty (default from config: auto)")
	c.Flags().BoolVar(&f.NoMouse, "no-mouse", false, "leave the mouse to the terminal (plain drag selects text; default from config)")
	c.Flags().DurationVar(&f.Reload, "reload", config.DefaultReload, "check the archive for changes at this interval and reload the view on screen when there are any; 0 switches it off, at least 1s (default from config: 5s)")
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
	projects, err := cfg.ProjectResolver()
	if err != nil {
		return tui.Options{}, errKind(ErrKindConfig, err)
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
	reload := cfg.TUI.Reload.Duration
	if f.ReloadSet {
		reload = f.Reload
		if err := config.ValidReload(reload); err != nil {
			return tui.Options{}, fmt.Errorf("--%w", err)
		}
	}
	return tui.Options{
		Location: cfg.Location(), Start: view, Query: f.Query, Graphics: g, NoMouse: noMouse, Reload: reload,
		DayStart: cfg.DayStartOffset(), Work: work, Holidays: holidays(cfg), HumanOnly: cfg.Work.HumanOnly(), Home: cfg.Home, Projects: projects,
		StatsPeriod: cfg.TUI.StatsPeriod, StatsDimension: cfg.TUI.StatsDimension, ActivityWindow: cfg.TUI.ActivityWindow, ActivityProjects: cfg.TUI.ActivityProjects,
		Keys: km,
	}, nil
}
