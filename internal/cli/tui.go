package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/tui"
)

func (e *env) tuiCmd() *cobra.Command {
	var query, graphics string
	c := &cobra.Command{
		Use:   "tui [search|stats|activity]",
		Short: "Interactive search, activity charts and first/last activity per day (spike)",
		Long: `tui opens the archive read-only in a full-screen terminal UI with a live
search view, an activity statistics view and a view of the first and the
last activity per day (see stats --workday). The optional argument selects
the view shown first.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			g, err := tui.ParseGraphics(graphics)
			if err != nil {
				return err
			}
			start := tui.ViewSearch
			if len(args) == 1 {
				start = args[0]
			}
			if !slices.Contains(tui.ViewNames, start) {
				return fmt.Errorf("unknown view %q (use %s)", start, strings.Join(tui.ViewNames, ", "))
			}
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			st, err := e.openRead(cfg)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			opts := tui.Options{Store: st, Location: cfg.Location(), Now: e.app.Now,
				Start: start, Query: query, Graphics: g, DayStart: cfg.DayStartOffset(), Work: workHours(cfg), Home: cfg.Home}
			return tui.Run(cmd.Context(), opts, e.app.Stdin, e.app.Stdout)
		},
	}
	c.Flags().StringVar(&query, "query", "", "initial search text")
	c.Flags().StringVar(&graphics, "graphics", "auto", "chart rendering: auto, text or kitty")
	return c
}
