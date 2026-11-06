package ingest

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sort"
	"time"

	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/store"
)

// Memory archives the project memory of every adapter that has one
// (harness.MemorySource), as All does when Deps.StoreMemory is set; the
// hook calls it after the transcripts. A scan is cheap: it compares size
// and mtime of every file with the stored state and reads only files that
// differ; the content is redacted with Redactor.Text before the store
// hashes it. A file that is gone, or every file of a project whose memory
// directory is gone, is recorded as deleted, unless the harness's data
// directory is missing altogether. One lock per harness keeps parallel
// hooks from scanning twice; a locked harness counts as Locked.
func Memory(ctx context.Context, d Deps) (Summary, error) {
	r := newRun(d)
	start := time.Now()
	err := r.memory(ctx)
	return r.finish(start), err
}

// memory scans the memory of every adapter; the error is only set for
// cancellation.
func (r *runner) memory(ctx context.Context) error {
	for _, a := range r.deps.Adapters {
		ms, ok := a.(harness.MemorySource)
		if !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		name := a.Name() + ": memory"
		unlock, ok, err := lockFile(r.deps.LockDir, "memory:"+a.Name())
		if err != nil {
			r.fail(name, err)
			continue
		}
		if !ok {
			r.sum.Locked++
			continue
		}
		err = r.memoryOf(ctx, a.Name(), ms)
		unlock()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if err != nil {
			r.fail(name, err)
		}
	}
	return nil
}

// memoryOf scans the memory of one harness and writes one update per
// project that changed.
func (r *runner) memoryOf(ctx context.Context, h string, ms harness.MemorySource) error {
	projects, complete, err := ms.Memory()
	if err != nil {
		return err
	}
	st := r.deps.Store
	state, err := st.MemoryState(ctx, h)
	if err != nil {
		return err
	}
	var candidates []string
	loaded := false
	seen := map[string]bool{}
	for _, p := range projects {
		if err := ctx.Err(); err != nil {
			return err
		}
		seen[p.Key] = true
		known, stored := state[p.Key]
		u := store.MemoryUpdate{Harness: h, Key: p.Key}
		onDisk := make(map[string]bool, len(p.Files))
		for _, f := range p.Files {
			onDisk[f.Path] = true
			// The store keeps mtimes to the millisecond (store.TimeFormat),
			// file systems to the nanosecond.
			if old, ok := known.Files[f.Path]; ok && old.SHA256 != "" && old.Size == f.Size && old.MTime.Equal(f.MTime.Truncate(time.Millisecond)) {
				continue
			}
			data, err := os.ReadFile(f.Abs)
			if errors.Is(err, fs.ErrNotExist) {
				continue // gone since the listing: the next scan records it
			}
			if err != nil {
				r.fail(f.Abs, err)
				continue
			}
			r.sum.MemoryRead++
			text := string(data)
			if rd := r.deps.Redactor; rd != nil {
				text = rd.Text(text)
			}
			u.Changes = append(u.Changes, store.MemoryChange{Path: f.Path, Size: f.Size, MTime: f.MTime, Content: []byte(text)})
		}
		u.Changes = append(u.Changes, deletions(known, onDisk)...)
		if known.Cwd == "" {
			if !loaded {
				if candidates, err = st.SessionCwds(ctx); err != nil {
					return err
				}
				loaded = true
			}
			u.Cwd = ms.MemoryCwd(p.Key, candidates)
			if u.Cwd != "" && r.deps.ResolveGit {
				if _, err := r.dir(ctx, u.Cwd); err != nil {
					return err
				}
			}
		}
		if stored && len(u.Changes) == 0 && u.Cwd == "" {
			continue
		}
		if err := r.writeMemory(ctx, u); err != nil {
			return err
		}
	}
	if !complete {
		return nil
	}
	keys := make([]string, 0, len(state))
	for k := range state {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if seen[k] {
			continue
		}
		if dels := deletions(state[k], nil); len(dels) > 0 {
			if err := r.writeMemory(ctx, store.MemoryUpdate{Harness: h, Key: k, Changes: dels}); err != nil {
				return err
			}
		}
	}
	return nil
}

// deletions returns a deletion for every existing file of a project that
// is not on disk, in path order.
func deletions(known store.MemoryProjectState, onDisk map[string]bool) []store.MemoryChange {
	var out []store.MemoryChange
	for p, f := range known.Files {
		if f.SHA256 != "" && !onDisk[p] {
			out = append(out, store.MemoryChange{Path: p, Deleted: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (r *runner) writeMemory(ctx context.Context, u store.MemoryUpdate) error {
	res, err := r.deps.Store.WriteMemory(ctx, u)
	if err != nil {
		return err
	}
	r.sum.MemoryVersions += res.Versions
	r.sum.MemoryDeleted += res.Deleted
	return nil
}
