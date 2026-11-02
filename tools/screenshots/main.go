// Command screenshots renders the TUI screenshots of the documentation; see
// internal/screenshot. It anonymizes a copy of the configured archive
// (never writing to it), builds the synthetic demo archive, drives the TUI
// headlessly for every shot, checks the frames for anything that looks
// like the original data and draws them as PNG files.
//
//	go run ./tools/screenshots -out docs/assets/screenshots
//
// The anonymized copy and the demo archive live in the work directory
// below the temp dir, never in the repository.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/screenshot"
	"github.com/seletz/total-recall/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "screenshots:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		live   = flag.String("live", "", "archive to anonymize (default: db of the config)")
		work   = flag.String("work", filepath.Join(os.TempDir(), "total-recall-screenshots"), "work directory for the anonymized copy, the demo archive and the frames")
		out    = flag.String("out", "docs/assets/screenshots", "directory of the PNG files")
		reuse  = flag.Bool("reuse", false, "keep an existing anonymized copy in the work directory")
		noPNG  = flag.Bool("no-png", false, "stop after the frames; write no images")
	)
	flag.Parse()
	ctx := context.Background()
	if *live == "" {
		c, err := config.Load(config.Options{})
		if err != nil {
			return err
		}
		*live = c.DB
	}
	if err := os.MkdirAll(*work, 0o755); err != nil {
		return err
	}

	anonPath := filepath.Join(*work, "anonymized.db")
	var needles []string
	if _, err := os.Stat(anonPath); err == nil && *reuse {
		fmt.Fprintf(os.Stderr, "reusing %s\n", anonPath)
	} else {
		fmt.Fprintf(os.Stderr, "anonymizing a copy of %s …\n", *live)
		start := time.Now()
		rep, err := screenshot.Anonymize(ctx, *live, anonPath)
		if err != nil {
			return err
		}
		needles = rep.Needles
		fmt.Fprintf(os.Stderr, "  %d sessions, %d events, %d names to check, %s\n", rep.Sessions, rep.Events, len(rep.Needles), time.Since(start).Round(time.Second))
	}
	needles = append(needles, machineNeedles()...)

	fmt.Fprintln(os.Stderr, "building the demo archive …")
	demoPath, err := screenshot.BuildDemo(ctx, filepath.Join(*work, "demo"))
	if err != nil {
		return err
	}

	anon, err := store.OpenReadOnly(anonPath)
	if err != nil {
		return err
	}
	defer func() { _ = anon.Close() }()
	demo, err := store.OpenReadOnly(demoPath)
	if err != nil {
		return err
	}
	defer func() { _ = demo.Close() }()

	fmt.Fprintln(os.Stderr, "rendering frames …")
	frames, err := screenshot.RenderAll(screenshot.Archives{Anon: anon, Demo: demo})
	if err != nil {
		return err
	}
	framesDir := filepath.Join(*work, "frames")
	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		return err
	}
	var leaks []string
	for _, f := range frames {
		plain := f.Plain()
		if hit := screenshot.ScanText(plain, needles); hit != "" {
			leaks = append(leaks, fmt.Sprintf("%s: %q", f.Shot.Name, hit))
		}
		base := filepath.Join(framesDir, f.Shot.Name)
		if err := os.WriteFile(base+".ansi", []byte(f.Content), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(base+".txt", []byte(plain+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "  %-16s %dx%d  %s\n", f.Shot.Name, f.Shot.Width, f.Shot.Height, f.Shot.Caption)
	}
	if len(leaks) > 0 {
		return fmt.Errorf("frames hold original names, nothing written to %s:\n  %s", *out, strings.Join(leaks, "\n  "))
	}
	if *noPNG {
		fmt.Fprintf(os.Stderr, "frames in %s\n", framesDir)
		return nil
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	for _, f := range frames {
		dst := filepath.Join(*out, f.Shot.Name+".png")
		if err := writePNG(dst, f); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "  wrote %s\n", dst)
	}
	return nil
}

func writePNG(path string, f screenshot.Frame) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := screenshot.PNG(file, f); err != nil {
		_ = file.Close()
		return fmt.Errorf("%s: %w", f.Shot.Name, err)
	}
	return file.Close()
}

// machineNeedles are strings that identify this machine and must not show
// up in a frame: the user name and the home directory, plus the macOS and
// Linux home prefixes.
func machineNeedles() []string {
	out := []string{"/Users/", "/home/"}
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		out = append(out, h)
	}
	if u, err := user.Current(); err == nil && len(u.Username) >= 4 {
		out = append(out, u.Username)
	}
	return out
}
