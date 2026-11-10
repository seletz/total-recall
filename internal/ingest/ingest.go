// Package ingest orchestrates reading transcripts into the store: it finds
// sources, locks each file, continues at the stored byte offset, redacts,
// resolves git info and writes events, sessions and the new offset in one
// transaction per batch (see docs/explanation/design.md, "Ingest, Hooks, Nebenläufigkeit").
package ingest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/dispatch"
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

// DefaultBatchBytes is the attachment data a batch holds before it is
// written even if it is shorter than BatchSize: a batch of 500 tool
// results with screenshots would otherwise sit in memory with their
// pictures.
const DefaultBatchBytes = 64 << 20

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
	// RawMaxBytes drops Event.Raw of lines longer than this many bytes and
	// sets Event.RawTruncated instead; 0 = no limit (the default since
	// #180: the archive keeps whole lines).
	RawMaxBytes int
	// StoreBlobs keeps Event.Attachments (images and documents of a line,
	// stored once per hash); false drops the data, raw keeps the
	// reference.
	StoreBlobs bool
	// ResolveGit enables git info resolution for session working directories.
	ResolveGit bool
	// GitResolve resolves a cwd; default gitinfo.Resolve.
	GitResolve func(ctx context.Context, cwd string) (gitinfo.Info, error)
	// LockDir holds the per-source lock files; required.
	LockDir string
	// BatchSize is the number of events per transaction; default DefaultBatchSize.
	BatchSize int
	// BatchBytes is the attachment data after which a batch is written
	// early; default DefaultBatchBytes.
	BatchBytes int64
	// Reread reads every source from the beginning instead of from its
	// stored offset. Events are idempotent, sessions merge: a reread adds
	// what earlier versions did not store (ingest --reread).
	Reread bool
	// RewriteRaw replaces raw and raw_truncated of events that already exist
	// with the freshly redacted line; text, actor and the full-text index
	// stay untouched. Only useful together with Reread, which is what makes
	// the existing events come by again (ingest --reread --rewrite-raw).
	RewriteRaw bool
	// StoreMemory makes All archive the project memory of the adapters
	// that have one (Memory, harness.MemorySource).
	StoreMemory bool
	// Dispatch tunes the recognition of inputs that another session typed
	// into this one (see package dispatch); the zero value takes the
	// defaults, a negative Window turns it off.
	Dispatch dispatch.Config
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
	// RawRewritten counts existing events and meta records whose raw was
	// replaced (Deps.RewriteRaw). It counts rows, each once per Run or All
	// call however often its record comes by, and none this call inserted.
	RawRewritten int
	// MetaRewritten is the part of RawRewritten that are meta records.
	MetaRewritten int
	// Dispatched counts human inputs of this call's time range that were
	// marked automation because another session sent the text (package
	// dispatch).
	Dispatched int
	// Meta counts meta records newly inserted (table session_meta).
	Meta int
	// MetaKept counts existing meta records a rewrite left as they were:
	// their stored line refers to more blobs than the new one, the
	// harness has deleted data the archive holds.
	MetaKept int
	// Blobs counts attachments newly stored, BlobBytes their size as
	// delivered (an attachment the archive already had does not count).
	Blobs     int
	BlobBytes int64
	Sessions  int // distinct sessions touched by new records
	// MemoryRead counts memory files read because their size or mtime
	// changed, MemoryVersions the new versions (contents) among them,
	// MemoryDeleted the files recorded as deleted (Memory).
	MemoryRead, MemoryVersions, MemoryDeleted int
	Errors                                    []FileError
	Duration                                  time.Duration
}

// runner is the state of one Run/All call.
type runner struct {
	deps     Deps
	sessions map[string]bool
	dirs     map[string]store.DirInfo // per-run cwd cache (also negative results)
	written  *store.WrittenRows       // rows written so far, with RewriteRaw
	sum      Summary
	lo, hi   time.Time // the time range of the events written so far
	err      error     // the failure of the current file's add, if any
}

func newRun(d Deps) *runner {
	if d.GitResolve == nil {
		d.GitResolve = gitinfo.Resolve
	}
	if d.BatchSize <= 0 {
		d.BatchSize = DefaultBatchSize
	}
	if d.BatchBytes <= 0 {
		d.BatchBytes = DefaultBatchBytes
	}
	if d.Logf == nil {
		d.Logf = func(string, ...any) {}
	}
	r := &runner{deps: d, sessions: map[string]bool{}, dirs: map[string]store.DirInfo{}}
	if d.RewriteRaw {
		r.written = store.NewWrittenRows()
	}
	return r
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
	r.markDispatched(ctx)
	return r.finish(start), nil
}

// All ingests every source of every adapter in d and, with
// d.StoreMemory, their project memory (Memory).
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
	r.markDispatched(ctx)
	if d.StoreMemory {
		if err := r.memory(ctx); err != nil {
			return r.finish(start), err
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

	r.err = nil
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
	if r.deps.Reread {
		offset = 0
	}
	if known && fi.Size() == offset && offset == prev.Offset {
		return false, nil
	}

	src := store.SourceState{Path: path, Harness: a.Name(), MTime: fi.ModTime()}
	var sessions []model.Session
	var events []model.Event
	var metas []model.Meta
	var pending int64 // attachment bytes in events and meta records
	flush := func(off int64) error {
		src.Offset = off
		src.Size = max(fi.Size(), off)
		res, err := st.WriteBatchOpts(ctx, src, sessions, events, store.WriteOptions{RewriteRaw: r.deps.RewriteRaw, Meta: metas, Written: r.written})
		if err != nil {
			return err
		}
		r.sum.Events += res.Inserted
		r.sum.RawRewritten += res.RawRewritten
		r.sum.MetaRewritten += res.MetaRewritten
		r.sum.Meta += res.MetaInserted
		r.sum.MetaKept += res.MetaKept
		r.sum.Blobs += res.Blobs
		r.sum.BlobBytes += res.BlobBytes
		sessions, events, metas, pending = sessions[:0], events[:0], metas[:0], 0
		return nil
	}

	end, perr := a.Parse(ctx, path, offset, func(rec model.Record) error {
		// Flush before a record that starts a new line, so the stored offset
		// always sits on a line boundary whose predecessors are all written.
		if off, ok := rec.Offset(); ok && (len(events)+len(metas) >= r.deps.BatchSize || pending >= r.deps.BatchBytes) {
			if err := flush(off); err != nil {
				return err
			}
		}
		pending += r.add(ctx, rec, &sessions, &events, &metas)
		return r.err
	})
	// Lines before end were fully emitted: persist them even if Parse failed.
	if err := flush(end); err != nil {
		return true, errors.Join(perr, err)
	}
	return true, perr
}

// add prepares one record (git info, redaction) and appends it to the
// batch. It returns the attachment bytes it added; a failure is left in
// r.err. Meta records follow StoreRaw and RawMaxBytes like raw: without
// StoreRaw none is kept, a line above the cap is dropped (logged) because
// the line is all a meta record has.
func (r *runner) add(ctx context.Context, rec model.Record, sessions *[]model.Session, events *[]model.Event, metas *[]model.Meta) int64 {
	s := rec.Session
	if s.ID != "" {
		r.sessions[s.ID] = true
		if s.Cwd != "" && r.deps.ResolveGit {
			d, err := r.dir(ctx, s.Cwd)
			if err != nil {
				r.err = err
				return 0
			}
			s.GitRoot, s.GitRemoteURL = d.GitRoot, d.GitRemoteURL
		}
		*sessions = append(*sessions, s)
	}
	// Events of one record usually share the raw line: redact it once.
	var rawIn, rawOut string
	var added int64
	for _, e := range rec.Events {
		r.sessions[e.SessionID] = true
		if !e.TS.IsZero() {
			if r.lo.IsZero() || e.TS.Before(r.lo) {
				r.lo = e.TS
			}
			if e.TS.After(r.hi) {
				r.hi = e.TS
			}
		}
		switch {
		case !r.deps.StoreRaw:
			e.Raw = ""
		case r.deps.RawMaxBytes > 0 && len(e.Raw) > r.deps.RawMaxBytes:
			e.Raw, e.RawTruncated = "", true
		}
		if !r.deps.StoreBlobs {
			e.Attachments = nil
		}
		for _, a := range e.Attachments {
			added += int64(len(a.Data))
		}
		if rd := r.deps.Redactor; rd != nil {
			e.Text = rd.Text(e.Text)
			if e.Raw != "" {
				if e.Raw != rawIn {
					rawIn = e.Raw
					// Invalid JSON is still masked as plain text; keep that result.
					rawOut, _ = rd.JSON(e.Raw)
				}
				e.Raw = rawOut
			}
		}
		*events = append(*events, e)
	}
	for _, m := range rec.Meta {
		switch {
		case !r.deps.StoreRaw:
			continue
		case r.deps.RawMaxBytes > 0 && len(m.Raw) > r.deps.RawMaxBytes:
			r.deps.Logf("ingest %s: meta record %s (%s) dropped: %d bytes over raw_max_bytes", m.SourcePath, m.ID, m.Type, len(m.Raw))
			continue
		}
		r.sessions[m.SessionID] = true
		// The attachments of a meta record are JSON (a system prompt, tool
		// definitions) or text (a backup copy of an edited file), redacted
		// like the line: they are kept whatever StoreBlobs says, else the
		// record would lose its content. Other data (a binary backup copy)
		// cannot be redacted and follows StoreBlobs like an image; the line
		// keeps the reference. Redaction changes the hash, so the reference
		// in the line follows.
		atts := make([]model.Attachment, 0, len(m.Attachments))
		for _, a := range m.Attachments {
			redactable := a.MediaType == "application/json" || strings.HasPrefix(a.MediaType, "text/")
			if !redactable && !r.deps.StoreBlobs {
				continue
			}
			if rd := r.deps.Redactor; rd != nil && redactable {
				var red string
				if a.MediaType == "application/json" {
					red, _ = rd.JSON(string(a.Data))
				} else {
					red = rd.Text(string(a.Data))
				}
				if red != string(a.Data) {
					h := blobref.Hash([]byte(red))
					m.Raw = strings.ReplaceAll(m.Raw, blobref.Ref{SHA256: a.SHA256}.String(), blobref.Ref{SHA256: h}.String())
					a.Data, a.SHA256 = []byte(red), h
				}
			}
			atts = append(atts, a)
			added += int64(len(a.Data))
		}
		m.Attachments = atts
		if rd := r.deps.Redactor; rd != nil {
			m.Raw, _ = rd.JSON(m.Raw)
		}
		*metas = append(*metas, m)
	}
	return added
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

// RegitSummary describes a Regit run.
type RegitSummary struct {
	Dirs     int // distinct session working directories looked at
	Sessions int // sessions whose git info changed
	Duration time.Duration
}

// Regit re-resolves git info for the working directory of every session,
// bypassing the dirs cache, and updates the cache and the sessions. A
// directory that no longer resolves (deleted worktree) or fails transiently
// keeps its cached result. Use it when git info in the archive is stale,
// e.g. after a remote was renamed or a resolution rule changed.
func Regit(ctx context.Context, d Deps) (RegitSummary, error) {
	r := newRun(d)
	start := time.Now()
	var sum RegitSummary
	cwds, err := d.Store.SessionCwds(ctx)
	if err != nil {
		return sum, err
	}
	for _, cwd := range cwds {
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		sum.Dirs++
		cached, ok, err := d.Store.Dir(ctx, cwd)
		if err != nil {
			return sum, err
		}
		info, gerr := r.deps.GitResolve(ctx, cwd)
		di := cached
		switch {
		case gerr == nil:
			di = store.DirInfo{Cwd: cwd, GitRoot: info.Root, GitRemoteURL: info.RemoteURL}
			if err := d.Store.PutDir(ctx, di); err != nil {
				return sum, err
			}
		case errors.Is(gerr, gitinfo.ErrNotRepo):
			if !ok || cached.GitRoot == "" {
				di = store.DirInfo{Cwd: cwd}
				if err := d.Store.PutDir(ctx, di); err != nil {
					return sum, err
				}
			}
		default:
			r.deps.Logf("regit: git info for %s: %v", cwd, gerr)
		}
		if di.GitRoot == "" {
			continue
		}
		n, err := d.Store.SetSessionGit(ctx, cwd, di.GitRoot, di.GitRemoteURL)
		if err != nil {
			return sum, err
		}
		sum.Sessions += int(n)
	}
	sum.Duration = time.Since(start)
	return sum, nil
}
