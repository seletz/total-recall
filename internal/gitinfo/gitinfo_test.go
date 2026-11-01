package gitinfo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNormalizeRemoteURL(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"scp ssh", "git@github.com:o/r.git", "https://github.com/o/r"},
		{"scp ssh no .git", "git@github.com:o/r", "https://github.com/o/r"},
		{"scp ssh uppercase host", "git@GitHub.com:o/r.git", "https://github.com/o/r"},
		{"ssh url", "ssh://git@github.com/o/r.git", "https://github.com/o/r"},
		{"ssh url with port", "ssh://git@git.example.org:2222/group/sub/r.git", "https://git.example.org/group/sub/r"},
		{"https", "https://github.com/o/r.git", "https://github.com/o/r"},
		{"https trailing slash", "https://github.com/o/r/", "https://github.com/o/r"},
		{"https credentials", "https://user:s3cret@github.com/o/r.git", "https://github.com/o/r"},
		{"https token only", "https://ghp_abc@github.com/o/r", "https://github.com/o/r"},
		{"http kept", "http://git.local/o/r.git", "http://git.local/o/r"},
		{"https port kept", "https://git.example.org:8443/o/r.git", "https://git.example.org:8443/o/r"},
		{"git protocol", "git://github.com/o/r.git", "https://github.com/o/r"},
		{"whitespace", "  git@github.com:o/r.git\n", "https://github.com/o/r"},
		{"local path", "/srv/git/r.git", "/srv/git/r.git"},
		{"file url", "file:///srv/git/r.git", "file:///srv/git/r.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeRemoteURL(tt.in); got != tt.want {
				t.Errorf("NormalizeRemoteURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.org",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.org")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// setupRepo creates repo "main" with one commit, an origin remote, a subdir,
// and a linked worktree "wt" on branch "feature/x".
func setupRepo(t *testing.T) (repo, wt string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo = filepath.Join(base, "main")
	wt = filepath.Join(base, "wt")
	if err := os.MkdirAll(filepath.Join(repo, "sub", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "init", "-q", "-b", "develop")
	run(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "init")
	run(t, repo, "remote", "add", "origin", "git@github.com:example/demo.git")
	run(t, repo, "worktree", "add", "-q", "-b", "feature/x", wt)
	return repo, wt
}

func TestResolve(t *testing.T) {
	requireGit(t)
	repo, wt := setupRepo(t)
	gone := filepath.Join(filepath.Dir(repo), "deleted-worktree", "a", "b")

	tests := []struct {
		name string
		cwd  string
		want Info
	}{
		{"repo root", repo, Info{Toplevel: repo, Root: repo, RemoteURL: "https://github.com/example/demo", Branch: "develop", Dir: repo}},
		{"subdir", filepath.Join(repo, "sub", "deeper"), Info{Toplevel: repo, Root: repo, RemoteURL: "https://github.com/example/demo", Branch: "develop", Dir: filepath.Join(repo, "sub", "deeper")}},
		{"worktree", wt, Info{Toplevel: wt, Root: repo, RemoteURL: "https://github.com/example/demo", Branch: "feature/x", Dir: wt}},
		{"missing dir inside repo", filepath.Join(repo, "sub", "gone", "x"), Info{Toplevel: repo, Root: repo, RemoteURL: "https://github.com/example/demo", Branch: "develop", Dir: filepath.Join(repo, "sub")}},
		{"missing dir outside repo", gone, Info{}},
		{"deleted sibling worktree <repo>__worktrees/<name>", filepath.Join(filepath.Dir(repo), "main__worktrees", "feature-y", "sub"),
			Info{Toplevel: repo, Root: repo, RemoteURL: "https://github.com/example/demo", Branch: "develop", Dir: repo}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(context.Background(), tt.cwd)
			if tt.want == (Info{}) {
				if !errors.Is(err, ErrNotRepo) {
					t.Fatalf("err = %v, want ErrNotRepo", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("Resolve(%q) =\n %+v\nwant\n %+v", tt.cwd, got, tt.want)
			}
		})
	}
}

func TestResolveNoRemoteDetached(t *testing.T) {
	requireGit(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "init")
	run(t, dir, "checkout", "-q", "--detach")
	got, err := Resolve(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Info{Toplevel: dir, Root: dir, Dir: dir}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestResolveEmptyCwd(t *testing.T) {
	if _, err := Resolve(context.Background(), ""); !errors.Is(err, ErrNotRepo) {
		t.Fatalf("err = %v, want ErrNotRepo", err)
	}
}
