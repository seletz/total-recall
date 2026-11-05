// Command screenshots renders the TUI screenshots of the documentation; see
// internal/screenshot. It drives the TUI headlessly against the synthetic
// demo archive for every shot, checks the frames for anything that looks
// like this machine, writes the text frames as the goldens of the
// screenshot tests and draws the frames as PNG files.
//
//	go run ./tools/screenshots -db build/demo.db -out docs/assets/screenshots -golden internal/screenshot/testdata/frames
//
// Without -db the demo archive is built in the work directory below the
// temp dir. Any other archive, such as an anonymized copy from
// tools/anonymize, can be passed instead; the goldens are then best left
// out (-golden "").
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/screenshot"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "screenshots:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		db     = flag.String("db", "", "archive to render (default: the demo archive, built in the work directory)")
		work   = flag.String("work", filepath.Join(os.TempDir(), "total-recall-screenshots"), "work directory for the frames and a built demo archive")
		out    = flag.String("out", "docs/assets/screenshots", "directory of the PNG files")
		golden = flag.String("golden", screenshot.GoldenDir, "directory of the text frames the tests compare with; empty writes none")
		noPNG  = flag.Bool("no-png", false, "stop after the frames; write no images")
	)
	flag.Parse()
	ctx := context.Background()
	if err := os.MkdirAll(*work, 0o755); err != nil {
		return err
	}
	if *db == "" {
		*db = filepath.Join(*work, "demo.db")
		fmt.Fprintf(os.Stderr, "building the demo archive at %s …\n", *db)
		sum, err := demo.Build(ctx, *db)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "  %d sessions, %d events\n", sum.Sessions, sum.Events)
	}
	homeDir := filepath.Join(*work, "env")
	if err := os.RemoveAll(homeDir); err != nil {
		return err
	}
	env, err := screenshot.NewEnv(*db, homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = env.Close() }()

	fmt.Fprintln(os.Stderr, "rendering frames …")
	frames, err := screenshot.RenderAll(env)
	if err != nil {
		return err
	}
	framesDir := filepath.Join(*work, "frames")
	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		return err
	}
	needles := machineNeedles()
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
		return fmt.Errorf("frames hold names of this machine, nothing written to %s:\n  %s", *out, strings.Join(leaks, "\n  "))
	}
	if *golden != "" {
		if err := screenshot.WriteGoldens(*golden, frames); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "goldens in %s\n", *golden)
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
// up in a frame: the user name and the home directory, plus the macOS home
// prefix. The demo's own home below /home is allowed.
func machineNeedles() []string {
	out := []string{"/Users/"}
	if h, err := os.UserHomeDir(); err == nil && h != "" && !strings.HasPrefix(demo.Home, h) {
		out = append(out, h)
	}
	if u, err := user.Current(); err == nil && len(u.Username) >= 4 && u.Username != "mara" {
		out = append(out, u.Username)
	}
	return out
}
