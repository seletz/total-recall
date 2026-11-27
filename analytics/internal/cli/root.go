// Package cli is the command line of total-recall-analytics.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/duck"
)

// App is one run of the command line.
type App struct {
	Args   []string
	Stdout io.Writer
	Stderr io.Writer
	// ExtDir overrides the cache directory of the embedded DuckDB extension
	// (tests); empty means duck.DefaultExtDir.
	ExtDir string
}

// Main runs the command line and returns the exit code.
func Main(a *App) int {
	if a.Stdout == nil {
		a.Stdout = os.Stdout
	}
	if a.Stderr == nil {
		a.Stderr = os.Stderr
	}
	root := newRoot(a)
	root.SetArgs(a.Args)
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)
	err := root.Execute()
	if err == nil {
		return 0
	}
	if !strings.Contains(err.Error(), "\n") && errorKind(err) == ErrKindInternal && isCobraUsage(err) {
		err = errKind(ErrKindUsage, err)
	}
	_, _ = fmt.Fprintln(a.Stderr, "total-recall-analytics:", err)
	if jsonRequested(a.Args) {
		writeJSONError(a.Stdout, err)
	}
	return 1
}

// isCobraUsage recognises the argument and flag errors cobra raises before a
// command runs.
func isCobraUsage(err error) bool {
	m := err.Error()
	for _, p := range []string{"unknown command", "unknown flag", "unknown shorthand flag", "flag needs an argument", "invalid argument", "accepts ", "requires "} {
		if strings.HasPrefix(m, p) || strings.Contains(m, " "+p) {
			return true
		}
	}
	return false
}

func newRoot(a *App) *cobra.Command {
	root := &cobra.Command{
		Use:           "total-recall-analytics",
		Short:         "Analyse total-recall archives of an agent pool with DuckDB",
		Long:          "total-recall-analytics builds an analytics database (star schema) from total-recall archives and queries it with DuckDB.\nIt reads the archives read-only and never contacts the network.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return errKind(ErrKindUsage, err)
	})
	root.AddCommand(newVersionCmd(a))
	return root
}

// open opens a DuckDB database with the embedded extension loaded.
func (a *App) open(ctx context.Context, o duck.Options) (*duck.DB, error) {
	if o.ExtDir == "" {
		o.ExtDir = a.ExtDir
	}
	db, err := duck.Open(ctx, o)
	return db, errKind(ErrKindDB, err)
}
