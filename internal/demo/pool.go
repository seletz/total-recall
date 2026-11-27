package demo

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/seletz/total-recall/internal/gitinfo"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/harness/claude"
	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/store"
)

// Pool is a demo agent pool (spike #347): the demo transcripts split by
// project directory over several hosts, each with its own local archive,
// plus the archive of all transcripts on one machine as the reference.
type Pool struct {
	// All is the archive of all transcripts (without the project memory).
	All string
	// Hosts maps each host name to its local archive.
	Hosts map[string]string
	// Projects maps each host name to the project directories it got.
	Projects map[string][]string
}

// BuildPool writes the demo transcripts below dir and ingests them into
// dir/all.db and, split by project directory (round robin in name order),
// into dir/<host>.db for each host. The project memory is left out: the
// prototype delivers transcripts only.
func BuildPool(ctx context.Context, dir string, hosts []string) (Pool, error) {
	p := Pool{All: filepath.Join(dir, "all.db"), Hosts: map[string]string{}, Projects: map[string][]string{}}
	projects, err := Write(filepath.Join(dir, "tree"))
	if err != nil {
		return p, err
	}
	defer func() { _ = os.RemoveAll(filepath.Join(dir, "tree")) }()
	entries, err := os.ReadDir(projects)
	if err != nil {
		return p, fmt.Errorf("demo: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for i, n := range names {
		h := hosts[i%len(hosts)]
		p.Projects[h] = append(p.Projects[h], n)
	}
	if err := ingestTree(ctx, p.All, projects); err != nil {
		return p, err
	}
	for _, h := range hosts {
		tree := filepath.Join(dir, "tree-"+h)
		for _, n := range p.Projects[h] {
			if err := copyTree(filepath.Join(projects, n), filepath.Join(tree, n)); err != nil {
				return p, err
			}
		}
		p.Hosts[h] = filepath.Join(dir, h+".db")
		if err := ingestTree(ctx, p.Hosts[h], tree); err != nil {
			return p, err
		}
		_ = os.RemoveAll(tree)
	}
	return p, nil
}

// ingestTree ingests the transcripts below projects into a new archive at
// path, the way Build does, without the project memory.
func ingestTree(ctx context.Context, path, projects string) error {
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("demo: %w", err)
		}
	}
	st, err := store.Open(path, store.WithClock(func() time.Time { return Now }))
	if err != nil {
		return fmt.Errorf("demo: %w", err)
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
		LockDir: filepath.Join(filepath.Dir(path), "locks-"+filepath.Base(path)),
	}
	sum, err := ingest.All(ctx, deps)
	if err != nil {
		return fmt.Errorf("demo: ingest: %w", err)
	}
	if len(sum.Errors) > 0 {
		return fmt.Errorf("demo: ingest %s: %w", sum.Errors[0].Path, sum.Errors[0].Err)
	}
	if err := st.Close(); err != nil {
		return fmt.Errorf("demo: %w", err)
	}
	_ = os.RemoveAll(deps.LockDir)
	_, err = relocate(ctx, path, projects, Projects)
	return err
}

func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
}
