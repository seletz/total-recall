package cli

import (
	"context"
	"fmt"
	"os"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/duck"
	"github.com/seletz/total-recall/analytics/internal/report"
	"github.com/seletz/total-recall/analytics/internal/star"
	"github.com/seletz/total-recall/analytics/internal/tui"
)

// tuiMetrics are the choices of --show, as the show row names them.
var tuiMetrics = []string{"events", "human", "agent", "sessions", "tool calls", "tokens", "hours"}

func newTUICmd(a *App) *cobra.Command {
	var show, by, period, host string
	var noMouse bool
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Browse the usage of the pool over time in the terminal",
		Long: `The usage of the analytics database as a chart over time with its totals:
what is counted (--show), how it is split (--by), the period and the host
are toggle rows, chosen with keys or a click; ←/→ and the mouse wheel move
the window of periods. The numbers are those of usage and worktime. The
database is opened only while a view loads, so build can run meanwhile;
r reloads.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !slices.Contains(tuiMetrics, show) {
				return usagef("--show %s: want %s", show, orList(tuiMetrics))
			}
			cfg, err := a.config()
			if err != nil {
				return err
			}
			path := a.analyticsPath(cfg)
			if _, err := os.Stat(path); err != nil {
				return errKind(ErrKindDB, fmt.Errorf("analytics database %s: %w (run build first)", path, star.ErrNotBuilt))
			}
			db, err := a.open(cmd.Context(), duck.Options{Path: path, ReadOnly: true})
			if err != nil {
				return err
			}
			tz, err := star.Setting(cmd.Context(), db, "tz")
			_ = db.Close()
			if err != nil {
				return errKind(ErrKindDB, err)
			}
			m := tui.New(tui.Options{
				Path: path, Home: cfg.Home, TZ: tz, Metric: show, By: by, Period: period, Host: host, NoMouse: noMouse,
				Load: func(ctx context.Context, run func(report.Querier) error) error {
					db, err := a.open(ctx, duck.Options{Path: path, ReadOnly: true})
					if err != nil {
						return err
					}
					defer func() { _ = db.Close() }()
					return run(db)
				},
			})
			_, err = tea.NewProgram(m, tea.WithContext(cmd.Context()), tea.WithInput(a.Stdin), tea.WithOutput(a.Stdout)).Run()
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&show, "show", "events", "what to count: "+orList(tuiMetrics))
	f.StringVar(&by, "by", "", "split by project, host, model, tool, account (hours: category, project, host); default the first")
	f.StringVar(&period, "period", "week", "day, week or month")
	f.StringVar(&host, "host", "", "only this host (default every host)")
	f.BoolVar(&noMouse, "no-mouse", false, "leave the terminal's mouse reports off")
	return cmd
}
