package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/duck"
	"github.com/seletz/total-recall/internal/version"
)

// VersionJSON is the output of `version --json`.
type VersionJSON struct {
	Version      string          `json:"version"`
	DuckDB       string          `json:"duckdb"`
	DuckDBSource string          `json:"duckdb_source"`
	Platform     string          `json:"platform"`
	Extensions   []ExtensionJSON `json:"extensions"`
}

// ExtensionJSON is a loaded DuckDB extension. Static means compiled into
// the binary; this build loads no other.
type ExtensionJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Static  bool   `json:"static"`
}

func newVersionCmd(a *App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the version, the linked DuckDB and the loaded extensions",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usagef("version takes no arguments, got %q", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			db, err := a.open(ctx, duck.Options{})
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			info, err := db.Info(ctx)
			if err != nil {
				return errKind(ErrKindDB, err)
			}
			v := VersionJSON{Version: version.Version, DuckDB: info.DuckDBVersion, DuckDBSource: info.SourceID, Platform: info.Platform, Extensions: []ExtensionJSON{}}
			for _, e := range info.Extensions {
				v.Extensions = append(v.Extensions, ExtensionJSON{Name: e.Name, Version: e.Version, Static: e.Static})
			}
			if asJSON {
				return writeJSON(a.Stdout, v)
			}
			tw := tabwriter.NewWriter(a.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintf(tw, "total-recall-analytics\t%s\n", v.Version)
			_, _ = fmt.Fprintf(tw, "DuckDB\t%s (%s, %s)\n", v.DuckDB, v.DuckDBSource, v.Platform)
			names := make([]string, 0, len(v.Extensions))
			for _, e := range v.Extensions {
				names = append(names, e.Name)
			}
			_, _ = fmt.Fprintf(tw, "extensions\t%s (compiled in)\n", strings.Join(names, ", "))
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}
