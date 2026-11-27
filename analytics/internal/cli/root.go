// Package cli is the command line of total-recall-analytics.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/duck"
)

// App is one run of the command line.
type App struct {
	Args   []string
	Stdout io.Writer
	Stderr io.Writer
	// Getenv looks up environment variables; default os.Getenv.
	Getenv func(string) string
	// Home overrides the home directory.
	Home string
	// Now is the clock; default time.Now.
	Now func() time.Time
	// Hostname names this host for build; default os.Hostname.
	Hostname func() (string, error)

	// global flags
	configFile string
	archive    string
	dbPath     string
	tz         string
}

// Main runs the command line and returns the exit code.
func Main(a *App) int {
	if a.Stdout == nil {
		a.Stdout = os.Stdout
	}
	if a.Stderr == nil {
		a.Stderr = os.Stderr
	}
	if a.Getenv == nil {
		a.Getenv = os.Getenv
	}
	if a.Now == nil {
		a.Now = time.Now
	}
	if a.Hostname == nil {
		a.Hostname = os.Hostname
	}
	root := newRoot(a)
	root.SetArgs(a.Args)
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)
	err := root.Execute()
	if err == nil {
		return 0
	}
	var ec exitCode
	if errors.As(err, &ec) {
		return int(ec)
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
	pf := root.PersistentFlags()
	pf.StringVar(&a.configFile, "config", "", "total-recall config file (default: TOTAL_RECALL_CONFIG or ~/.config/total-recall/config.toml)")
	pf.StringVar(&a.archive, "archive", "", "total-recall archive to load (default: db of the config)")
	pf.StringVar(&a.dbPath, "db", "", "analytics database (default: analytics.duckdb next to the archive)")
	pf.StringVar(&a.tz, "tz", "", "primary time zone (default: timezone of the config)")
	root.AddCommand(newVersionCmd(a), newBuildCmd(a), newStatusCmd(a), newCheckCmd(a), newViewsCmd(a),
		newQueryCmd(a), newExportCmd(a), newUsageCmd(a), newTokensCmd(a), newWorktimeCmd(a))
	return root
}

// open opens a DuckDB database.
func (a *App) open(ctx context.Context, o duck.Options) (*duck.DB, error) {
	db, err := duck.Open(ctx, o)
	return db, errKind(ErrKindDB, err)
}
