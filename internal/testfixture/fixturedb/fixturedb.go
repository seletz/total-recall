// Package fixturedb ingests the synthetic transcripts of package testfixture
// into a temporary database, so store, CLI and TUI tests can share one
// populated store. It lives in its own package because the ingest and cli
// test packages import testfixture and must not form an import cycle.
package fixturedb

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/seletz/total-recall/internal/gitinfo"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/harness/claude"
	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
)

// Ingest reads every transcript below projects (a Claude Code projects tree
// as written by testfixture.Demo or testfixture.Activity) into a fresh
// database below t.TempDir() and returns the open read-write store and the
// database path. The store is closed when the test ends.
//
// Git resolution is deterministic: cwds found in git resolve to that info,
// all others are treated as "not a repository"; real git never runs. A nil
// map marks every directory as not a repository.
func Ingest(t testing.TB, projects string, git map[string]gitinfo.Info) (*store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("fixturedb: open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	deps := ingest.Deps{
		Store:      st,
		Adapters:   []harness.Adapter{claude.New(projects)},
		StoreRaw:   true,
		ResolveGit: true,
		GitResolve: func(_ context.Context, cwd string) (gitinfo.Info, error) {
			if info, ok := git[cwd]; ok {
				return info, nil
			}
			return gitinfo.Info{}, gitinfo.ErrNotRepo
		},
		LockDir: filepath.Join(dir, "locks"),
	}
	sum, err := ingest.All(context.Background(), deps)
	if err != nil {
		t.Fatalf("fixturedb: ingest: %v", err)
	}
	for _, e := range sum.Errors {
		t.Errorf("fixturedb: ingest %s: %v", e.Path, e.Err)
	}
	if t.Failed() {
		t.FailNow()
	}
	return st, path
}

// Activity writes testfixture.Activity into a temp dir and ingests it with
// testfixture.ActivityGit. See testfixture.Activity for the documented counts.
func Activity(t testing.TB) (*store.Store, string) {
	t.Helper()
	return Ingest(t, testfixture.Activity(t, t.TempDir()), testfixture.ActivityGit())
}

// Demo writes testfixture.Demo into a temp dir and ingests it; no directory
// resolves to a git repository.
func Demo(t testing.TB) (*store.Store, string) {
	t.Helper()
	return Ingest(t, testfixture.Demo(t, t.TempDir()), nil)
}
