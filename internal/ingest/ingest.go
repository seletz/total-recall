// Package ingest orchestrates reading transcripts into the store: it finds
// sources, locks each file, continues at the stored byte offset, redacts,
// resolves git info and writes events, sessions and the new offset in one
// transaction per batch (see docs/design.md, "Ingest, Hooks, Nebenläufigkeit").
package ingest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/seletz/total-recall/internal/gitinfo"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/redact"
	"github.com/seletz/total-recall/internal/store"
)

// NegativeDirTTL is how long a cached "not a git repository" result is
// trusted before the directory is resolved again (positive results are kept
// forever, they must survive deleted worktrees).
const NegativeDirTTL = 7 * 24 * time.Hour

// DefaultBatchSize is the number of events written per transaction.
const DefaultBatchSize = 500

// Deps are the collaborators of an ingest run.
type Deps struct {
	// Store is the open (read-write) archive.
	Store *store.Store
	// Adapters are the enabled harness adapters.
	Adapters []harness.Adapter
	// Redactor masks Event.Text and Event.Raw; nil disables redaction.
	Redactor *redact.Redactor
	// StoreRaw keeps Event.Raw; false drops it.
	StoreRaw bool
	// ResolveGit enables git info resolution for session working directories.
	ResolveGit bool
	// GitResolve resolves a cwd; default gitinfo.Resolve.
	GitResolve func(ctx context.Context, cwd string) (gitinfo.Info, error)
	// LockDir holds the per-source lock files; required.
	LockDir string
	// BatchSize is the number of events per transaction; default DefaultBatchSize.
	BatchSize int
	// Logf receives diagnostics; default discard.
	Logf func(format string, args ...any)
}

// FileError is a failure while ingesting one source.
type FileError struct {
	Path string
	Err  error
}

// Summary describes an ingest run.
type Summary struct {
	Scanned   int // sources looked at
	Changed   int // sources with new data read
	Unchanged int // sources without new data
	Locked    int // sources skipped because another process holds the lock
	Events    int // events newly inserted
	Sessions  int // distinct sessions touched by new records
	Errors    []FileError
	Duration  time.Duration
}

// runner is the state of one Run/All call.
type runner struct {
	deps     Deps
	sessions map[string]bool
	dirs     map[string]store.DirInfo // per-run cwd cache (also negative results)
	sum      Summary
}

func newRun(d Deps) *runner {
	if d.GitResolve == nil {
		d.GitResolve = gitinfo.Resolve
	}
	if d.BatchSize <= 0 {
		d.BatchSize = DefaultBatchSize
	}
	if d.Logf == nil {
		d.Logf = func(string, ...any) {}
	}
	return &runner{deps: d, sessions: map[string]bool{}, dirs: map[string]store.DirInfo{}}
}

// PathOwner is optionally implemented by adapters to claim explicit paths
// given to Run when more than one adapter is enabled.
type PathOwner interface {
	Owns(path string) bool
}

// Run ingests the given transcript files. Per-file errors are collected in
// the summary; the returned error is only set for setup problems or
// cancellation.
func Run(ctx context.Context, d Deps, paths []string) (Summary, error) {
	r := newRun(d)
	start := time.Now()
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return r.finish(start), err
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			r.fail(p, err)
			continue
		}
		a, err := r.adapterFor(abs)
		if err != nil {
			r.fail(abs, err)
			continue
		}
		r.file(ctx, a, abs)
	}
	return r.finish(start), nil
}

// All ingests every source of every adapter in d.
func All(ctx context.Context, d Deps) (Summary, error) {
	r := newRun(d)
	start := time.Now()
	for _, a := range d.Adapters {
		srcs, err := a.Sources()
		if err != nil {
			r.fail(a.Name()+": sources", err)
			continue
		}
		for _, p := range srcs {
			if err := ctx.Err(); err != nil {
				return r.finish(start), err
			}
			r.file(ctx, a, p)
		}
	}
	return r.finish(start), nil
}

func (r *runner) finish(start time.Time) Summary {
	r.sum.Sessions = len(r.sessions)
	r.sum.Duration = time.Since(start)
	return r.sum
}

func (r *runner) fail(path string, err error) {
	r.sum.Errors = append(r.sum.Errors, FileError{Path: path, Err: err})
	r.deps.Logf("ingest %s: %v", path, err)
}

func (r *runner) adapterFor(path string) (harness.Adapter, error) {
	for _, a := range r.deps.Adapters {
		if o, ok := a.(PathOwner); ok && o.Owns(path) {
			return a, nil
		}
	}
	if len(r.deps.Adapters) == 1 {
		return r.deps.Adapters[0], nil
	}
	return nil, fmt.Errorf("no harness adapter for %s", path)
}

// file ingests one source and records the outcome in the summary.
func (r *runner) file(ctx context.Context, a harness.Adapter, path string) {
	r.sum.Scanned++
	unlock, ok, err := lockFile(r.deps.LockDir, path)
	if err != nil {
		r.fail(path, err)
		return
	}
	if !ok {
		r.sum.Locked++
		return
	}
	defer unlock()

	changed, err := r.ingestFile(ctx, a, path)
	if changed {
		r.sum.Changed++
	} else if err == nil {
		r.sum.Unchanged++
	}
	if err != nil {
		r.fail(path, err)
	}
}

// ingestFile reads path from its stored offset. It reports whether the file
// had anything to read.
func (r *runner) ingestFile(ctx context.Context, a harness.Adapter, path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	st := r.deps.Store
	prev, known, err := st.Source(ctx, path)
	if err != nil {
		return false, err
	}
	offset := prev.Offset
	if fi.Size() < offset {
		r.deps.Logf("ingest %s: size %d < offset %d, re-reading from 0", path, fi.Size(), offset)
		offset = 0
	}
	if known && fi.Size() == offset && offset == prev.Offset {
		return false, nil
	}

	src := store.SourceState{Path: path, Harness: a.Name(), MTime: fi.ModTime()}
	var sessions []model.Session
	var events []model.Event
	flush := func(off int64) error {
		src.Offset = off
		src.Size = max(fi.Size(), off)
		n, err := st.WriteBatchN(ctx, src, sessions, events)
		if err != nil {
			return err
		}
		r.sum.Events += n
		sessions, events = sessions[:0], events[:0]
		return nil
	}

	end, perr := a.Parse(ctx, path, offset, func(rec model.Record) error {
		// Flush before a record that starts a new line, so the stored offset
		// always sits on a line boundary whose predecessors are all written.
		if len(events) >= r.deps.BatchSize && len(rec.Events) > 0 {
			if err := flush(rec.Events[0].SourceOffset); err != nil {
				return err
			}
		}
		return r.add(ctx, rec, &sessions, &events)
	})
	// Lines before end were fully emitted: persist them even if Parse failed.
	if err := flush(end); err != nil {
		return true, errors.Join(perr, err)
	}
	return true, perr
}

// add prepares one record (git info, redaction) and appends it to the batch.
func (r *runner) add(ctx context.Context, rec model.Record, sessions *[]model.Session, events *[]model.Event) error {
	s := rec.Session
	if s.ID != "" {
		r.sessions[s.ID] = true
		if s.Cwd != "" && r.deps.ResolveGit {
			d, err := r.dir(ctx, s.Cwd)
			if err != nil {
				return err
			}
			s.GitRoot, s.GitRemoteURL = d.GitRoot, d.GitRemoteURL
		}
		*sessions = append(*sessions, s)
	}
	for _, e := range rec.Events {
		r.sessions[e.SessionID] = true
		if !r.deps.StoreRaw {
			e.Raw = ""
		}
		if rd := r.deps.Redactor; rd != nil {
			e.Text = rd.Text(e.Text)
			if e.Raw != "" {
				// Invalid JSON is still masked as plain text; keep that result.
				e.Raw, _ = rd.JSON(e.Raw)
			}
		}
		*events = append(*events, e)
	}
	return nil
}

// dir returns git info for cwd: per-run cache, then the store's dirs table,
// then git. Not-a-repo results are cached as empty rows.
func (r *runner) dir(ctx context.Context, cwd string) (store.DirInfo, error) {
	if d, ok := r.dirs[cwd]; ok {
		return d, nil
	}
	d, ok, err := r.deps.Store.Dir(ctx, cwd)
	if err != nil {
		return store.DirInfo{}, err
	}
	if ok && d.GitRoot == "" && time.Since(d.ResolvedAt) > NegativeDirTTL {
		ok = false
	}
	if !ok {
		info, gerr := r.deps.GitResolve(ctx, cwd)
		d = store.DirInfo{Cwd: cwd, GitRoot: info.Root, GitRemoteURL: info.RemoteURL}
		switch {
		case gerr == nil, errors.Is(gerr, gitinfo.ErrNotRepo):
			if err := r.deps.Store.PutDir(ctx, d); err != nil {
				return store.DirInfo{}, err
			}
		default:
			// Transient failure (e.g. timeout): do not persist, retry next run.
			r.deps.Logf("ingest: git info for %s: %v", cwd, gerr)
		}
	}
	r.dirs[cwd] = d
	return d, nil
}
