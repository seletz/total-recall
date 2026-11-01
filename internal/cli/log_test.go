package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogRotation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "total-recall.log")
	l := newLogger(p, func() time.Time { return time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) })
	if l.maxSize != defaultLogMaxSize || defaultLogMaxSize != 5<<20 {
		t.Fatalf("default max size = %d", l.maxSize)
	}
	l.maxSize = 200
	for i := 0; i < 20; i++ {
		l.Errorf("line %02d %s", i, strings.Repeat("x", 20))
	}
	cur, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(p + ".1")
	if err != nil {
		t.Fatalf("no rotated log: %v", err)
	}
	if len(cur) > 200+80 || len(old) > 200+80 {
		t.Errorf("sizes after rotation: current %d, old %d", len(cur), len(old))
	}
	if !strings.Contains(string(cur), "line 19") {
		t.Errorf("newest line missing from current log:\n%s", cur)
	}
	if strings.Contains(string(old), "line 00") {
		t.Errorf("more than one old generation kept:\n%s", old)
	}
	if m, _ := filepath.Glob(p + ".2"); len(m) > 0 {
		t.Errorf("second generation exists: %v", m)
	}
	// lastErrors still reads the current file.
	errs, err := lastErrors(p, 1)
	if err != nil || len(errs) != 1 || !strings.Contains(errs[0], "line 19") {
		t.Errorf("lastErrors = %v %v", errs, err)
	}
}
