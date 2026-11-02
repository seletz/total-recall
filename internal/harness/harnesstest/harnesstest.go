// Package harnesstest is the conformance suite for harness adapters. Every
// adapter runs it from its own tests with a set of synthetic fixtures:
//
//	func TestConformance(t *testing.T) {
//		harnesstest.Run(t, func(dir string) harness.Adapter { return myharness.New(dir) }, "testdata/projects")
//	}
//
// The suite copies the fixture tree to a temporary directory for every check
// (so it may truncate and rewrite files) and verifies the adapter contract
// that ingest relies on (see docs/explanation/design.md, "Neue Harness anbinden"):
//
//   - Sources is deterministic and returns absolute, existing, unique paths.
//   - Parse consumes complete lines only: a partial trailing line is left for
//     the next call, re-parsing from the returned offset emits nothing new,
//     and parsing a file in two parts yields the same events as parsing it at
//     once.
//   - Event IDs are unique and stable across adapter instances and runs.
//   - Every event has an ID and session ID prefixed with "<Name()>:", a
//     timestamp, and a Kind, Role and Actor from the sets defined in package
//     model; session IDs of records carry the same prefix.
//   - A cancelled context stops Parse with context.Canceled, an emit error is
//     returned as is, and in both cases resuming from the returned offset
//     neither loses nor repeats events.
//
// Token counts (TokensIn/TokensOut) are not compared between split and
// whole-file parses: adapters may attribute usage per Parse call.
package harnesstest

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/model"
)

// Factory returns an adapter whose Sources are the transcript files below dir.
type Factory func(dir string) harness.Adapter

// check is one part of the contract.
type check struct {
	name string
	fn   func(t testing.TB, f Factory, dir string)
}

var checks = []check{
	{"sources deterministic", checkSources},
	{"event fields", checkEventFields},
	{"event IDs unique and stable", checkIDs},
	{"offsets at end of file", checkReparse},
	{"partial trailing line", checkPartialLastLine},
	{"two halves equal whole", checkSplit},
	{"context cancellation", checkCancel},
	{"emit error", checkEmitError},
}

// Run checks the adapter contract for the adapters built by factory on a copy
// of the fixture tree below fixtures. The fixtures must contain at least one
// source with two or more lines that produce events, and every source must end
// with a newline.
func Run(t *testing.T, factory Factory, fixtures string) {
	t.Helper()
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			c.fn(t, factory, copyTree(t, fixtures))
		})
	}
}

// eventKey is what must be identical between two parses of the same data.
type eventKey struct {
	ID, SessionID string
	TS            time.Time
	Kind          model.Kind
	Role          string
	Actor         model.Actor
	Text          string
	ToolName      string
}

func keyOf(e model.Event) eventKey {
	return eventKey{e.ID, e.SessionID, e.TS, e.Kind, e.Role, e.Actor, e.Text, e.ToolName}
}

// parse runs Parse and collects records.
func parse(ctx context.Context, a harness.Adapter, path string, off int64) ([]model.Record, int64, error) {
	var recs []model.Record
	end, err := a.Parse(ctx, path, off, func(r model.Record) error {
		recs = append(recs, r)
		return nil
	})
	return recs, end, err
}

func keys(recs []model.Record) []eventKey {
	var out []eventKey
	for _, r := range recs {
		for _, e := range r.Events {
			out = append(out, keyOf(e))
		}
	}
	return out
}

func sources(t testing.TB, a harness.Adapter) []string {
	t.Helper()
	srcs, err := a.Sources()
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	if len(srcs) == 0 {
		t.Fatalf("Sources: no sources in the fixtures")
	}
	return srcs
}

func mustParse(t testing.TB, a harness.Adapter, path string, off int64) ([]model.Record, int64) {
	t.Helper()
	recs, end, err := parse(context.Background(), a, path, off)
	if err != nil {
		t.Fatalf("Parse(%s, %d): %v", path, off, err)
	}
	return recs, end
}

func checkSources(t testing.TB, f Factory, dir string) {
	a := f(dir)
	first := sources(t, a)
	again := sources(t, a)
	fresh := sources(t, f(dir))
	if !slices.Equal(first, again) || !slices.Equal(first, fresh) {
		t.Errorf("Sources not deterministic:\n%v\n%v\n%v", first, again, fresh)
	}
	seen := map[string]bool{}
	for _, p := range first {
		if !filepath.IsAbs(p) {
			t.Errorf("source %q is not absolute", p)
		}
		if seen[p] {
			t.Errorf("source %q listed twice", p)
		}
		seen[p] = true
		if fi, err := os.Stat(p); err != nil || fi.IsDir() {
			t.Errorf("source %q is not a file: %v", p, err)
		}
	}
}

func checkEventFields(t testing.TB, f Factory, dir string) {
	a := f(dir)
	prefix := a.Name() + ":"
	if a.Name() == "" || strings.Contains(a.Name(), ":") {
		t.Fatalf("Name() = %q: must be non-empty without ':'", a.Name())
	}
	events := 0
	for _, p := range sources(t, a) {
		recs, _ := mustParse(t, a, p, 0)
		for _, r := range recs {
			if r.Session.ID != "" && !strings.HasPrefix(r.Session.ID, prefix) {
				t.Errorf("%s: session ID %q lacks prefix %q", p, r.Session.ID, prefix)
			}
			if r.Session.ParentID != "" && !strings.HasPrefix(r.Session.ParentID, prefix) {
				t.Errorf("%s: parent ID %q lacks prefix %q", p, r.Session.ParentID, prefix)
			}
			if r.Session.ID != "" && r.Session.Harness != "" && r.Session.Harness != a.Name() {
				t.Errorf("%s: session harness %q, want %q", p, r.Session.Harness, a.Name())
			}
			for _, e := range r.Events {
				events++
				if !strings.HasPrefix(e.ID, prefix) || len(e.ID) == len(prefix) {
					t.Errorf("%s: event ID %q lacks prefix %q", p, e.ID, prefix)
				}
				if !strings.HasPrefix(e.SessionID, prefix) || len(e.SessionID) == len(prefix) {
					t.Errorf("%s: event %s: session ID %q lacks prefix %q", p, e.ID, e.SessionID, prefix)
				}
				if e.TS.IsZero() {
					t.Errorf("%s: event %s has no timestamp", p, e.ID)
				}
				if !e.Kind.Valid() {
					t.Errorf("%s: event %s: invalid kind %q", p, e.ID, e.Kind)
				}
				if !model.ValidRole(e.Role) {
					t.Errorf("%s: event %s: invalid role %q", p, e.ID, e.Role)
				}
				if !e.Actor.Valid() {
					t.Errorf("%s: event %s: invalid actor %q", p, e.ID, e.Actor)
				}
			}
		}
	}
	if events == 0 {
		t.Errorf("fixtures produced no events")
	}
}

func checkIDs(t testing.TB, f Factory, dir string) {
	collect := func(a harness.Adapter) []eventKey {
		var all []eventKey
		for _, p := range sources(t, a) {
			recs, _ := mustParse(t, a, p, 0)
			all = append(all, keys(recs)...)
		}
		return all
	}
	a := f(dir)
	first := collect(a)
	seen := map[string]bool{}
	for _, k := range first {
		if seen[k.ID] {
			t.Errorf("event ID %q emitted twice", k.ID)
		}
		seen[k.ID] = true
	}
	if again := collect(a); !slices.Equal(first, again) {
		t.Errorf("second run with the same adapter differs")
	}
	if fresh := collect(f(dir)); !slices.Equal(first, fresh) {
		t.Errorf("run with a new adapter differs")
	}
}

func checkReparse(t testing.TB, f Factory, dir string) {
	a := f(dir)
	for _, p := range sources(t, a) {
		b := readFile(t, p)
		_, end := mustParse(t, a, p, 0)
		if want := int64(len(b)); end != want {
			t.Errorf("%s: Parse returned offset %d, want file size %d", p, end, want)
		}
		recs, end2 := mustParse(t, a, p, end)
		if len(keys(recs)) != 0 {
			t.Errorf("%s: re-parse from %d emitted %d events", p, end, len(keys(recs)))
		}
		if end2 != end {
			t.Errorf("%s: re-parse from %d returned %d", p, end, end2)
		}
	}
}

// splitParse writes the first cut bytes of p, parses them, restores the
// whole file and parses from the returned offset. It checks offset
// semantics and that both parts together equal a whole-file parse.
func splitParse(t testing.TB, a harness.Adapter, p string, full []byte, cut int) {
	t.Helper()
	whole, wholeEnd := mustParse(t, a, p, 0)
	writeFile(t, p, full[:cut])
	first, off := mustParse(t, a, p, 0)
	lineStart := int64(bytes.LastIndexByte(full[:cut], '\n') + 1)
	if off != lineStart {
		t.Errorf("%s cut at %d: Parse returned offset %d, want %d (end of the last complete line)", p, cut, off, lineStart)
	}
	writeFile(t, p, full)
	rest, end := mustParse(t, a, p, off)
	if end != wholeEnd {
		t.Errorf("%s cut at %d: second Parse returned %d, want %d", p, cut, end, wholeEnd)
	}
	got := append(keys(first), keys(rest)...)
	if want := keys(whole); !slices.Equal(got, want) {
		t.Errorf("%s cut at %d: parts emitted %d events, whole file %d, or they differ", p, cut, len(got), len(want))
	}
}

// lineStarts returns the offsets at which the lines of b start.
func lineStarts(b []byte) []int {
	starts := []int{0}
	for i, c := range b {
		if c == '\n' && i+1 < len(b) {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func checkPartialLastLine(t testing.TB, f Factory, dir string) {
	a := f(dir)
	for _, p := range sources(t, a) {
		full := readFile(t, p)
		if len(full) == 0 {
			continue
		}
		starts := lineStarts(full)
		last := starts[len(starts)-1]
		splitParse(t, a, p, full, last+(len(full)-last)/2)
	}
}

func checkSplit(t testing.TB, f Factory, dir string) {
	a := f(dir)
	split := 0
	for _, p := range sources(t, a) {
		full := readFile(t, p)
		starts := lineStarts(full)
		if len(starts) < 2 {
			continue
		}
		split++
		mid := starts[len(starts)/2]
		splitParse(t, a, p, full, mid)                            // at a line boundary
		splitParse(t, a, p, full, mid+(lineEnd(full, mid)-mid)/2) // inside a line
		splitParse(t, a, p, full, starts[len(starts)/2-1]+1)      // one byte into the previous line
	}
	if split == 0 {
		t.Errorf("fixtures need a source with at least two lines")
	}
}

func lineEnd(b []byte, start int) int {
	if i := bytes.IndexByte(b[start:], '\n'); i >= 0 {
		return start + i + 1
	}
	return len(b)
}

// multiEventSource returns a source whose whole-file parse emits events in
// at least two records.
func multiEventSource(t testing.TB, a harness.Adapter) (string, []eventKey) {
	t.Helper()
	for _, p := range sources(t, a) {
		recs, _ := mustParse(t, a, p, 0)
		n := 0
		for _, r := range recs {
			if len(r.Events) > 0 {
				n++
			}
		}
		if n >= 2 {
			return p, keys(recs)
		}
	}
	t.Fatalf("fixtures need a source with at least two records that produce events")
	return "", nil
}

func checkCancel(t testing.TB, f Factory, dir string) {
	a := f(dir)
	p, whole := multiEventSource(t, a)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recs, off, err := parse(ctx, a, p, 0)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Parse with cancelled context: err = %v, want context.Canceled", err)
	}
	if len(recs) != 0 || off != 0 {
		t.Errorf("Parse with cancelled context emitted %d records, offset %d", len(recs), off)
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	var first []model.Record
	off, err = a.Parse(ctx, p, 0, func(r model.Record) error {
		first = append(first, r)
		if len(r.Events) > 0 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Parse cancelled during emit: err = %v, want context.Canceled", err)
	}
	if got := len(keys(first)); got >= len(whole) {
		t.Errorf("Parse cancelled during emit still emitted all %d events", got)
	}
	resumeEquals(t, a, p, off, keys(first), whole)
}

func checkEmitError(t testing.TB, f Factory, dir string) {
	a := f(dir)
	p, whole := multiEventSource(t, a)
	boom := errors.New("emit failed")
	var accepted []eventKey
	failed := false
	off, err := a.Parse(context.Background(), p, 0, func(r model.Record) error {
		if len(r.Events) > 0 && len(accepted) > 0 && !failed {
			failed = true
			return boom
		}
		accepted = append(accepted, keys([]model.Record{r})...)
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Parse with failing emit: err = %v, want the emit error", err)
	}
	resumeEquals(t, a, p, off, accepted, whole)
}

// resumeEquals parses p from off and checks that before plus the new events
// equal whole: nothing lost, nothing repeated.
func resumeEquals(t testing.TB, a harness.Adapter, p string, off int64, before, whole []eventKey) {
	t.Helper()
	rest, _ := mustParse(t, a, p, off)
	got := append(slices.Clone(before), keys(rest)...)
	if !slices.Equal(got, whole) {
		t.Errorf("resuming from offset %d: %d+%d events, want the %d of a whole-file parse in order",
			off, len(before), len(keys(rest)), len(whole))
	}
}

func readFile(t testing.TB, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeFile(t testing.TB, p string, b []byte) {
	t.Helper()
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// copyTree copies the regular files below src into a new temp dir.
func copyTree(t testing.TB, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixtures %s: %v", src, err)
	}
	return dst
}
