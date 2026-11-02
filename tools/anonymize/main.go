// Command anonymize copies an archive with every name and text removed,
// see screenshot.Anonymize: projects get neutral names by activity rank,
// titles, texts, tool names and paths are dropped and the full-text index
// is rebuilt empty. The source is opened read-only and never written. It
// is an optional local tool to look at the stats of one's own archive
// without its contents, for example with
//
//	go run ./tools/anonymize -out /tmp/anon.db
//	total-recall --db /tmp/anon.db tui stats
//
// or to render the screenshots from it (tools/screenshots -db). The docs
// use the synthetic demo archive instead (tools/demo).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/screenshot"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "anonymize:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		live = flag.String("live", "", "archive to copy (default: db of the config)")
		out  = flag.String("out", "", "path of the anonymized copy (required, replaced if it exists)")
	)
	flag.Parse()
	if *out == "" {
		return fmt.Errorf("-out is required")
	}
	if *live == "" {
		c, err := config.Load(config.Options{})
		if err != nil {
			return err
		}
		*live = c.DB
	}
	fmt.Fprintf(os.Stderr, "anonymizing a copy of %s …\n", *live)
	start := time.Now()
	rep, err := screenshot.Anonymize(context.Background(), *live, *out)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  %d sessions, %d events, %s\n", rep.Sessions, rep.Events, time.Since(start).Round(time.Second))
	fmt.Fprintf(os.Stderr, "  %d names removed, none of them left in the copy:\n  %s\n", len(rep.Needles), strings.Join(rep.Needles, ", "))
	fmt.Fprintf(os.Stderr, "wrote %s\n", *out)
	return nil
}
