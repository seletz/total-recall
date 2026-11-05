package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogRotation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "total-recall.log")
	l := New(p, func() time.Time { return time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) })
	if l.maxSize != DefaultMaxSize || DefaultMaxSize != 5<<20 {
		t.Fatalf("default max size = %d", l.maxSize)
	}
	l.SetMaxSize(200)
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
	// ReadErrors still reads the current file.
	errs, err := ReadErrors(p)
	if err != nil || len(errs) == 0 || !strings.Contains(errs[len(errs)-1].Message, "line 19") {
		t.Errorf("ReadErrors = %v %v", errs, err)
	}
}

func TestReadErrors(t *testing.T) {
	now := time.Date(2026, 11, 2, 20, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)
	const (
		stale   = "2026-11-01T10:00:00Z ERROR hook SubagentStop: /p/x/y.jsonl: stat /p/x/y.jsonl: no such file or directory\n"
		old     = "2026-11-01T12:00:00Z ERROR hook Stop: /p/x/z.jsonl: permission denied\n"
		info    = "2026-11-01T21:00:00Z INFO hook SubagentStop: transcript not present, skipped: /p/x/a.jsonl\n"
		fresh   = "2026-11-02T17:00:00Z ERROR hook Stop: boom\n"
		fresh2  = "2026-11-02T19:30:00Z ERROR backup: disk full\n"
		noStamp = "ERROR panic: runtime error\n"
		other   = "2026-11-02T19:40:00Z INFO ingest: 3 files, 12 events (0.2s)\n"
	)
	tests := []struct {
		name       string
		log        string
		wantRecent []string
		wantOlder  int
	}{
		{"no file", "", nil, 0},
		{"only old", stale + old, nil, 1},
		{"only fresh", fresh + fresh2, []string{"hook Stop: boom", "backup: disk full"}, 0},
		{"info skips are not errors", info + other, nil, 0},
		{"missing transcript never counts", stale + "2026-11-02T18:00:00Z ERROR hook SubagentStop: /p/x/q.jsonl: stat /p/x/q.jsonl: no such file or directory\n", nil, 0},
		{"mixed", stale + old + info + fresh + other + fresh2, []string{"hook Stop: boom", "backup: disk full"}, 1},
		{"no timestamp counts as recent", old + noStamp, []string{"panic: runtime error"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "total-recall.log")
			if tc.log != "" {
				if err := os.WriteFile(p, []byte(tc.log), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			all, err := ReadErrors(p)
			if err != nil {
				t.Fatal(err)
			}
			recent, older := SplitErrors(all, since)
			var msgs []string
			for _, e := range recent {
				msgs = append(msgs, e.Message)
				if !e.At.IsZero() && e.At.Before(since) {
					t.Errorf("recent error %v is before %v", e.At, since)
				}
			}
			if strings.Join(msgs, "|") != strings.Join(tc.wantRecent, "|") || older != tc.wantOlder {
				t.Errorf("recent = %q, older = %d; want %q, %d", msgs, older, tc.wantRecent, tc.wantOlder)
			}
		})
	}
	// Timestamps are parsed.
	p := filepath.Join(t.TempDir(), "total-recall.log")
	if err := os.WriteFile(p, []byte(fresh), 0o644); err != nil {
		t.Fatal(err)
	}
	all, err := ReadErrors(p)
	if err != nil || len(all) != 1 || !all[0].At.Equal(time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC)) {
		t.Errorf("ReadErrors = %+v, %v", all, err)
	}
}
