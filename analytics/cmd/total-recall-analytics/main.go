// Command total-recall-analytics analyses total-recall archives with DuckDB.
//
// It is a separate binary because the DuckDB driver needs cgo; total-recall
// itself stays a static, cgo-free build.
package main

import (
	"os"

	"github.com/seletz/total-recall/analytics/internal/cli"
)

func main() {
	os.Exit(cli.Main(&cli.App{Args: os.Args[1:]}))
}
