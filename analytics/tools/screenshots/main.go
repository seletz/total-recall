// Command screenshots renders the screenshots of the TUI of
// total-recall-analytics (package shots) as PNGs for the docs and writes
// the text goldens of the shots test. It is `mise run
// analytics:screenshots`; everything comes from the synthetic demo pool.
//
//	go run ./tools/screenshots -out ../docs/assets/analytics-screenshots -golden internal/shots/testdata/frames
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/seletz/total-recall/analytics/internal/shots"
	"github.com/seletz/total-recall/internal/screenshot"
)

func main() {
	out := flag.String("out", "../docs/assets/analytics-screenshots", "directory of the PNGs")
	golden := flag.String("golden", "internal/shots/testdata/frames", "directory of the text goldens; empty writes none")
	flag.Parse()
	if err := run(*out, *golden); err != nil {
		fmt.Fprintln(os.Stderr, "screenshots:", err)
		os.Exit(1)
	}
}

func run(out, golden string) error {
	dir, err := os.MkdirTemp("", "analytics-shots-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	db, _, err := shots.Demo(context.Background(), dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	var frames []screenshot.Frame
	for _, s := range shots.Shots {
		f, err := shots.Render(s, db)
		if err != nil {
			return err
		}
		frames = append(frames, f)
		p := filepath.Join(out, s.Name+".png")
		w, err := os.Create(p)
		if err != nil {
			return err
		}
		if err := screenshot.PNG(w, f); err != nil {
			_ = w.Close()
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		fmt.Println("wrote", p)
	}
	if golden != "" {
		return screenshot.WriteGoldens(golden, frames)
	}
	return nil
}
