package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/logfile"
	"github.com/seletz/total-recall/internal/ops"
)

func (e *env) reindexCmd() *cobra.Command {
	var actors, dryRun bool
	c := &cobra.Command{
		Use:   "reindex",
		Short: "Recompute derived fields of stored events",
		Long: `Recompute derived fields of events already in the archive, without
reading the transcripts again.

  reindex --actors         apply the current actor rules (built-in and
                           [harness.<name>.actors] in the configuration) to
                           every stored user message and update the actors
                           that differ; idempotent
  --dry-run                only report how many events would change`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !actors {
				return fmt.Errorf("reindex: give --actors")
			}
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			log := logfile.New(cfg.LogPath(), e.app.Now)
			st, err := ops.OpenWrite(cfg, log)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			d, err := ops.Deps(cfg, st, log)
			if err != nil {
				return err
			}
			sum, err := ingest.Reclassify(cmd.Context(), d, dryRun)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(e.out(cfg), ops.FormatReclassify(sum))
			return nil
		},
	}
	f := c.Flags()
	f.BoolVar(&actors, "actors", false, "reclassify the actor of every stored user message with the current rules")
	f.BoolVar(&dryRun, "dry-run", false, "only count what would change, write nothing")
	return c
}
