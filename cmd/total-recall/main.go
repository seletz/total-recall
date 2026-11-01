// Command total-recall archives coding-agent sessions into SQLite.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/version"
)

func main() {
	root := &cobra.Command{
		Use:          "total-recall",
		Short:        "Archive and query coding-agent sessions",
		SilenceUsage: true,
	}
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run:   func(*cobra.Command, []string) { fmt.Println(version.Version) },
	})
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
