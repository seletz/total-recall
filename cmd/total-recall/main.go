// Command total-recall archives coding-agent sessions into SQLite.
package main

import (
	"os"

	"github.com/seletz/total-recall/internal/cli"
)

func main() {
	os.Exit(cli.Main(&cli.App{Args: os.Args[1:]}))
}
