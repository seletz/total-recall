// Package fixturedb ingests the synthetic transcripts of package testfixture
// into a temporary database and opens the demo archive of package demo, so
// store, CLI and TUI tests can share one populated store. It lives in its own package because the ingest and cli
// test packages import testfixture and must not form an import cycle.
package fixturedb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/gitinfo"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/harness/claude"
	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
)

// Ingest reads every transcript below projects (a Claude Code projects tree
// as written by testfixture.Small or testfixture.Activity) into a fresh
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
		StoreBlobs: true,
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

// Small writes testfixture.Small into a temp dir and ingests it; no
// directory resolves to a git repository.
func Small(t testing.TB) (*store.Store, string) {
	t.Helper()
	return Ingest(t, testfixture.Small(t, t.TempDir()), nil)
}

// DemoEnv names the environment variable that points Demo at a prebuilt
// demo archive, as written by `mise run demo:db` (build/demo.db); the
// mise tasks and the CI set it. Unset, every test builds its own copy.
const DemoEnv = "TOTAL_RECALL_DEMO_DB"

// Demo opens the demo archive of package demo read-only and returns the
// store and the path. With DemoEnv set, the prebuilt archive it names is
// opened (missing: the test fails with a hint); otherwise the archive is
// built below t.TempDir(), which takes about a second. Both are the same
// archive: the generator is deterministic.
func Demo(t testing.TB) (*store.Store, string) {
	t.Helper()
	path := os.Getenv(DemoEnv)
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("fixturedb: %s=%s: %v (run `mise run demo:db` or unset the variable)", DemoEnv, path, err)
		}
	} else {
		path = filepath.Join(t.TempDir(), "demo.db")
		if _, err := demo.Build(context.Background(), path); err != nil {
			t.Fatalf("fixturedb: build demo archive: %v", err)
		}
	}
	st, err := store.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("fixturedb: open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, path
}

// DemoMain is for the TestMain of a package whose tests share the demo
// archive: when DemoEnv is unset, it builds the archive once below the
// temp directory, points DemoEnv at it for the run and removes it after.
// It returns the exit code for os.Exit.
func DemoMain(m *testing.M) int {
	if os.Getenv(DemoEnv) != "" {
		return m.Run()
	}
	dir, err := os.MkdirTemp("", "total-recall-demo-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fixturedb:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "demo.db")
	if _, err := demo.Build(context.Background(), path); err != nil {
		fmt.Fprintln(os.Stderr, "fixturedb: build demo archive:", err)
		return 1
	}
	if err := os.Setenv(DemoEnv, path); err != nil {
		fmt.Fprintln(os.Stderr, "fixturedb:", err)
		return 1
	}
	return m.Run()
}
