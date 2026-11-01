package harnesstest

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/model"
)

// toy is a minimal adapter for lines "<session> <RFC3339 time> <text>" in
// *.log files. Its flags break one part of the contract each.
type toy struct {
	dir string
	bug string

	mu    sync.Mutex
	calls int
}

func (a *toy) Name() string { return "toy" }

func (a *toy) Sources() ([]string, error) {
	abs, err := filepath.Abs(a.dir)
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.Walk(abs, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && strings.HasSuffix(p, ".log") {
			out = append(out, p)
		}
		return err
	})
	sort.Strings(out)
	a.mu.Lock()
	a.calls++
	if a.bug == "unstable sources" && a.calls%2 == 0 {
		sort.Sort(sort.Reverse(sort.StringSlice(out)))
	}
	a.mu.Unlock()
	return out, err
}

func (a *toy) Parse(ctx context.Context, path string, offset int64, emit func(model.Record) error) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return offset, err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset, err
	}
	r := bufio.NewReader(f)
	for {
		if a.bug != "ignores context" {
			if err := ctx.Err(); err != nil {
				return offset, err
			}
		}
		line, err := r.ReadBytes('\n')
		if errors.Is(err, io.EOF) && (a.bug != "consumes partial line" || len(line) == 0) {
			return offset, nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return offset, err
		}
		next := offset + int64(len(line))
		parts := strings.SplitN(string(bytes.TrimRight(line, "\n")), " ", 3)
		if len(parts) == 3 {
			ts, _ := time.Parse(time.RFC3339, parts[1])
			sid := "toy:" + parts[0]
			if a.bug == "no prefix" {
				sid = parts[0]
			}
			id := fmt.Sprintf("toy:%s:%d", filepath.Base(path), offset)
			if a.bug == "unstable IDs" {
				a.mu.Lock()
				a.calls++
				id += fmt.Sprintf("#%d", a.calls)
				a.mu.Unlock()
			}
			kind := model.KindMessage
			if a.bug == "bad kind" {
				kind = "chat"
			}
			e := model.Event{ID: id, SessionID: sid, TS: ts, Kind: kind, Role: "user", Actor: model.ActorHuman, Text: parts[2]}
			if err := emit(model.Record{Session: model.Session{ID: sid, Harness: "toy"}, Events: []model.Event{e}}); err != nil {
				if a.bug != "swallows emit error" {
					return offset, err
				}
			}
		}
		offset = next
	}
}

func factory(bug string) Factory {
	return func(dir string) harness.Adapter { return &toy{dir: dir, bug: bug} }
}

func TestToyAdapterConforms(t *testing.T) {
	Run(t, factory(""), "testdata/toy")
}

// recorder captures failures of a check instead of failing the test.
type recorder struct {
	testing.TB
	mu     sync.Mutex
	failed []string
}

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = append(r.failed, fmt.Sprintf(format, args...))
}
func (r *recorder) Error(args ...any) { r.Errorf("%s", fmt.Sprint(args...)) }
func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	runtime.Goexit()
}
func (r *recorder) Fatal(args ...any) { r.Fatalf("%s", fmt.Sprint(args...)) }

// failures runs every check against f and returns the names of failing checks.
func failures(t *testing.T, f Factory) []string {
	var out []string
	for _, c := range checks {
		r := &recorder{TB: t}
		done := make(chan struct{})
		go func() {
			defer close(done)
			c.fn(r, f, copyTree(t, "testdata/toy"))
		}()
		<-done
		if len(r.failed) > 0 {
			out = append(out, c.name)
		}
	}
	return out
}

func TestBrokenAdaptersFail(t *testing.T) {
	tests := []struct {
		bug  string
		want string // a check that must fail
	}{
		{"unstable sources", "sources deterministic"},
		{"no prefix", "event fields"},
		{"bad kind", "event fields"},
		{"unstable IDs", "event IDs unique and stable"},
		{"consumes partial line", "partial trailing line"},
		{"ignores context", "context cancellation"},
		{"swallows emit error", "emit error"},
	}
	for _, tt := range tests {
		t.Run(tt.bug, func(t *testing.T) {
			got := failures(t, factory(tt.bug))
			found := false
			for _, n := range got {
				found = found || n == tt.want
			}
			if !found {
				t.Errorf("check %q did not fail; failing checks: %v", tt.want, got)
			}
		})
	}
}
