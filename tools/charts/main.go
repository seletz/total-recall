// Command charts renders the figures of `total-recall chart` for the
// documentation from the synthetic demo archive: October 2026 of the
// fictional developer, with the categories and colours of the chart tests,
// as SVG files below the output directory, in English and, below de/, in
// German.
//
//	go run ./tools/charts -db build/demo.db -out docs/assets/charts
//
// Without -db the demo archive is built in a temporary directory. The
// files are byte-stable (SVG text, fixed clock), so `mise run
// docs:charts:check` (scripts/charts-check.sh) compares a fresh render
// with the committed ones; the CI runs it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/seletz/total-recall/internal/cli"
	"github.com/seletz/total-recall/internal/demo"
)

// Month is the month of the figures: the last full month of the archive.
const Month = "2026-10"

// Config is the configuration the figures are drawn with: the demo
// archive, no git resolution, one category with a colour per project
// family of the fictional developer.
const Config = `db = %q
timezone = "Europe/Berlin"

[git]
resolve = false

[[worktime.category]]
name = "kompass"
match = ["https://git.invalid/mara/kompass"]
color = "blue"

[[worktime.category]]
name = "privat"
match = ["~/dotfiles", "~/notizen"]
color = "purple"
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "charts:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		db  = flag.String("db", "", "archive to render (default: the demo archive, built in a temporary directory)")
		out = flag.String("out", "docs/assets/charts", "directory of the SVG files")
	)
	flag.Parse()
	work, err := os.MkdirTemp("", "total-recall-charts")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()
	if *db == "" {
		*db = filepath.Join(work, "demo.db")
		fmt.Fprintf(os.Stderr, "building the demo archive at %s …\n", *db)
		if _, err := demo.Build(context.Background(), *db); err != nil {
			return err
		}
	}
	abs, err := filepath.Abs(*db)
	if err != nil {
		return err
	}
	cfg := filepath.Join(work, "config.toml")
	if err := os.WriteFile(cfg, fmt.Appendf(nil, Config, abs), 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	for _, r := range []struct{ lang, dir string }{{"en", *out}, {"de", filepath.Join(*out, "de")}} {
		app := &cli.App{
			Args:   []string{"--config", cfg, "chart", "all", "--month", Month, "--format", "svg", "--lang", r.lang, "--out", r.dir},
			Home:   demo.Home,
			Getenv: func(k string) string { return "" },
			Now:    func() time.Time { return demo.Now },
			Stdout: os.Stderr,
			Stderr: os.Stderr,
		}
		if code := cli.Main(app); code != 0 {
			return fmt.Errorf("total-recall chart --lang %s failed with exit code %d", r.lang, code)
		}
	}
	return nil
}
