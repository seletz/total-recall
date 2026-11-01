package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/tui"
)

func (e *env) tuiCmd() *cobra.Command {
	var query, graphics string
	c := &cobra.Command{
		Use:   "tui [search|stats]",
		Short: "Interactive search and activity charts (spike)",
		Long: `tui opens the archive read-only in a full-screen terminal UI with a live
search view and an activity statistics view. The optional argument selects
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
			if start != tui.ViewSearch && start != tui.ViewStats {
				return fmt.Errorf("unknown view %q (use %s or %s)", start, tui.ViewSearch, tui.ViewStats)
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
				Start: start, Query: query, Graphics: g}
			return tui.Run(cmd.Context(), opts, e.app.Stdin, e.app.Stdout)
		},
	}
	c.Flags().StringVar(&query, "query", "", "initial search text")
	c.Flags().StringVar(&graphics, "graphics", "auto", "chart rendering: auto, text or kitty")
	return c
}
