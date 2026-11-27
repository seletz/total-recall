package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/star"
)

// ViewJSON is a published view with its columns (`views --json`).
type ViewJSON struct {
	View    string       `json:"view"`
	Columns []ColumnJSON `json:"columns"`
}

// ColumnJSON is a column of a view.
type ColumnJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func newViewsCmd(a *App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "views [VIEW...]",
		Short: "List the published views and their columns (the contract)",
		Long: `Lists the views analytics_*_v1, the interface of the analytics database for
query, export and BI tools, with the columns and types of their contract.
Needs no database.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			want, missing := map[string]bool{}, map[string]bool{}
			for _, v := range args {
				want[v], missing[v] = true, true
			}
			out := []ViewJSON{}
			for _, v := range star.Views() {
				if len(want) > 0 && !want[v.Name] {
					continue
				}
				delete(missing, v.Name)
				j := ViewJSON{View: v.Name, Columns: []ColumnJSON{}}
				for _, c := range v.Columns {
					j.Columns = append(j.Columns, ColumnJSON{Name: c.Name, Type: c.Type})
				}
				out = append(out, j)
			}
			for v := range missing {
				return usagef("no view %s (see views)", v)
			}
			if asJSON {
				return writeJSON(a.Stdout, out)
			}
			for i, v := range out {
				if i > 0 {
					_, _ = fmt.Fprintln(a.Stdout)
				}
				_, _ = fmt.Fprintln(a.Stdout, v.View)
				for _, c := range v.Columns {
					_, _ = fmt.Fprintf(a.Stdout, "  %-20s %s\n", c.Name, c.Type)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}
