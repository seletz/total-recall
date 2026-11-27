package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/star"
)

// ExportJSON is one written file (`export --json` writes a list).
type ExportJSON struct {
	View string `json:"view"`
	Path string `json:"path"`
	Rows int64  `json:"rows"`
}

func newExportCmd(a *App) *cobra.Command {
	var asJSON bool
	var format, outDir string
	cmd := &cobra.Command{
		Use:   "export --out DIR [VIEW...]",
		Short: "Export the published views as Parquet or CSV files",
		Long: `Writes each view (default: all analytics_*_v1) to DIR/<view>.parquet or
.csv, for notebooks and BI tools. Existing files are replaced.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if outDir == "" {
				return usagef("export needs --out DIR")
			}
			if format != "parquet" && format != "csv" {
				return usagef("--format %s: want parquet or csv", format)
			}
			known := map[string]bool{}
			var views []string
			for _, v := range star.Views() {
				known[v.Name] = true
				views = append(views, v.Name)
			}
			if len(args) > 0 {
				views = nil
				for _, v := range args {
					if !known[v] {
						return usagef("no view %s (see views)", v)
					}
					views = append(views, v)
				}
			}
			ctx := cmd.Context()
			cfg, err := a.config()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return err
			}
			db, _, err := a.openAnalytics(ctx, cfg, true)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			out := []ExportJSON{}
			for _, v := range views {
				path, err := filepath.Abs(filepath.Join(outDir, v+"."+format))
				if err != nil {
					return err
				}
				opt := "FORMAT parquet"
				if format == "csv" {
					opt = "FORMAT csv, HEADER true"
				}
				var n int64
				if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+v).Scan(&n); err != nil {
					return errKind(ErrKindSQL, err)
				}
				if _, err := db.ExecContext(ctx, "COPY (SELECT * FROM "+v+") TO '"+strings.ReplaceAll(path, "'", "''")+"' ("+opt+")"); err != nil {
					return errKind(ErrKindSQL, err)
				}
				out = append(out, ExportJSON{View: v, Path: path, Rows: n})
			}
			if asJSON {
				return writeJSON(a.Stdout, out)
			}
			tw := tabwriter.NewWriter(a.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "VIEW\tROWS\tFILE")
			for _, e := range out {
				_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\n", e.View, e.Rows, e.Path)
			}
			return tw.Flush()
		},
	}
	f := cmd.Flags()
	f.BoolVar(&asJSON, "json", false, "output as JSON")
	f.StringVar(&format, "format", "parquet", "parquet or csv")
	f.StringVar(&outDir, "out", "", "directory for the files")
	return cmd
}
