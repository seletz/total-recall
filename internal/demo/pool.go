package demo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/seletz/total-recall/internal/gitinfo"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/harness/claude"
	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/store"
)

// PoolHosts are the hosts of the demo pool: host-a is the notebook of the
// fictional developer, host-b an agent box that runs the generated
// sessions of the kompass worktree and of wetterwarte. kompass is worked
// on from both. The written-out conversations (see Query) stay on host-a:
// a coordinator there hands prompts to workers in the worktree, and a
// prompt is only recognised as a peer's when both sessions are in the same
// archive.
var PoolHosts = []string{"host-a", "host-b"}

// Pool is a built demo pool.
type Pool struct {
	// All is the archive of every transcript of the pool on one machine:
	// the reference the hosts add up to.
	All string
	// Hosts are the archives of the hosts, in the order of PoolHosts.
	Hosts []PoolArchive
}

// PoolArchive is the archive of one host of the pool.
type PoolArchive struct {
	Host, Path string
	Summary
}

// PoolArchiveID is the fixed archive ID of the pool archive name (a host
// of PoolHosts, or "all").
func PoolArchiveID(name string) string {
	switch name {
	case "host-a":
		return "de00000000000000000000000000b00a"
	case "host-b":
		return "de00000000000000000000000000b00b"
	}
	return "de00000000000000000000000000b0ff"
}

// hostOf returns the host of the pool s ran on (see PoolHosts).
func hostOf(s session) string {
	if s.num > 0 && (s.cwd == kompassWorktree.cwd || s.cwd == wetterwarte.cwd) {
		return "host-b"
	}
	return "host-a"
}

// BuildPool writes the demo pool to dir: one archive per host
// (dir/<host>.db) and the reference archive dir/all.db, replacing
// existing ones. The pool holds the sessions of the demo archive, split by
// hostOf (see PoolHosts), with models and tokens on every agent record (see ModelLarge);
// the project memory and the file history are left out. A session is
// continued only on its own host, so a continuation to the other host is
// dropped there. The build is deterministic.
func BuildPool(ctx context.Context, dir string) (Pool, error) {
	p := Pool{All: filepath.Join(dir, "all.db")}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return p, fmt.Errorf("demo: %w", err)
	}
	tmp, err := os.MkdirTemp("", "total-recall-pool-")
	if err != nil {
		return p, fmt.Errorf("demo: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	sessions := plan()
	host := map[string]string{}
	for _, s := range sessions {
		host[s.id] = hostOf(s)
	}
	write := func(name string, keep func(session) bool) (string, error) {
		projects := filepath.Join(tmp, name, "projects")
		for _, s := range sessions {
			if !keep(s) {
				continue
			}
			if name != "all" && s.continuedIn != "" && host[s.continuedIn] != name {
				s.continuedIn = ""
			}
			if err := writeSession(projects, s, style{usage: true}); err != nil {
				return "", err
			}
		}
		return projects, nil
	}
	projects, err := write("all", func(session) bool { return true })
	if err != nil {
		return p, err
	}
	if _, err := ingestPool(ctx, p.All, "", PoolArchiveID("all"), projects, filepath.Join(tmp, "locks-all")); err != nil {
		return p, err
	}
	for _, h := range PoolHosts {
		projects, err := write(h, func(s session) bool { return host[s.id] == h })
		if err != nil {
			return p, err
		}
		a := PoolArchive{Host: h, Path: filepath.Join(dir, h+".db")}
		if a.Summary, err = ingestPool(ctx, a.Path, h, PoolArchiveID(h), projects, filepath.Join(tmp, "locks-"+h)); err != nil {
			return p, err
		}
		p.Hosts = append(p.Hosts, a)
	}
	return p, nil
}

// ingestPool ingests the transcripts below projects into a new archive at
// path the way Build does, without the project memory and the file
// history, as the archive id of host ("": none).
func ingestPool(ctx context.Context, path, host, id, projects, locks string) (Summary, error) {
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return Summary{}, fmt.Errorf("demo: %w", err)
		}
	}
	st, err := store.Open(path, store.WithClock(func() time.Time { return Now }), store.WithHost(host))
	if err != nil {
		return Summary{}, fmt.Errorf("demo: %w", err)
	}
	defer func() { _ = st.Close() }()
	git := Git()
	deps := ingest.Deps{
		Store:      st,
		Adapters:   []harness.Adapter{claude.New(projects)},
		StoreRaw:   true,
		StoreBlobs: true,
		ResolveGit: true,
		GitResolve: func(_ context.Context, cwd string) (gitinfo.Info, error) {
			if info, ok := git[cwd]; ok {
				return info, nil
			}
			return gitinfo.Info{}, gitinfo.ErrNotRepo
		},
		LockDir: locks,
	}
	sum, err := ingest.All(ctx, deps)
	if err != nil {
		return Summary{}, fmt.Errorf("demo: ingest: %w", err)
	}
	if len(sum.Errors) > 0 {
		return Summary{}, fmt.Errorf("demo: ingest %s: %w", sum.Errors[0].Path, sum.Errors[0].Err)
	}
	if err := st.Close(); err != nil {
		return Summary{}, fmt.Errorf("demo: %w", err)
	}
	return relocate(ctx, path, projects, Projects, id)
}
