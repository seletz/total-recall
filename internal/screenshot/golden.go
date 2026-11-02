package screenshot

import (
	"fmt"
	"os"
	"path/filepath"
)

// GoldenDir is where the text frames of the shots are checked in, relative
// to this package: one <shot>.txt per shot, written by tools/screenshots
// together with the images and compared by TestFramesGolden.
const GoldenDir = "testdata/frames"

// GoldenPath returns the golden file of a shot below dir.
func GoldenPath(dir, name string) string { return filepath.Join(dir, name+".txt") }

// WriteGoldens writes the plain text frames below dir, one file per shot,
// and removes the files of shots that no longer exist.
func WriteGoldens(dir string, frames []Frame) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("goldens: %w", err)
	}
	keep := map[string]bool{}
	for _, f := range frames {
		p := GoldenPath(dir, f.Shot.Name)
		keep[filepath.Base(p)] = true
		if err := os.WriteFile(p, []byte(f.Plain()+"\n"), 0o644); err != nil {
			return fmt.Errorf("goldens: %w", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("goldens: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".txt" && !keep[e.Name()] {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return fmt.Errorf("goldens: %w", err)
			}
		}
	}
	return nil
}
