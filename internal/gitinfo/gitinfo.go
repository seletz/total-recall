// Package gitinfo resolves a working directory to its git repository facts:
// toplevel, main repository root (also for linked worktrees), normalized origin
// URL and current branch. It shells out to the git binary.
package gitinfo

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrNotRepo is returned when neither cwd nor any existing ancestor is inside a git repository.
var ErrNotRepo = errors.New("gitinfo: not a git repository")

// Timeout bounds every single git invocation.
const Timeout = 5 * time.Second

// Info describes the git repository a directory belongs to. Empty fields are unknown.
type Info struct {
	// Dir is the existing directory git was actually run in (cwd or its nearest existing ancestor).
	Dir string
	// Toplevel is the working tree root (for a linked worktree: the worktree's root).
	Toplevel string
	// Root is the main repository root; equals Toplevel unless Dir is in a linked worktree.
	Root string
	// RemoteURL is the normalized URL of remote "origin", without credentials.
	RemoteURL string
	// Branch is the current branch; empty for a detached HEAD.
	Branch string
}

// Resolve returns git facts for cwd. If cwd no longer exists (e.g. a deleted
// worktree), the nearest existing ancestor directory is used instead.
func Resolve(ctx context.Context, cwd string) (Info, error) {
	if cwd == "" {
		return Info{}, ErrNotRepo
	}
	dir := nearestExisting(filepath.Clean(cwd))
	if dir == "" {
		return Info{}, ErrNotRepo
	}
	top, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || top == "" {
		return Info{}, ErrNotRepo
	}
	info := Info{Dir: dir, Toplevel: top, Root: top}
	if common, err := git(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil && common != "" {
		info.Root = mainRoot(common, top)
	}
	if u, err := git(ctx, dir, "remote", "get-url", "origin"); err == nil {
		info.RemoteURL = NormalizeRemoteURL(u)
	}
	if b, err := git(ctx, dir, "branch", "--show-current"); err == nil {
		info.Branch = b
	}
	return info, nil
}

// mainRoot derives the main repository root from the absolute common git dir.
// A non-bare repository keeps its common dir in "<root>/.git"; for anything else
// (bare repositories, unusual layouts) the toplevel is the best answer.
func mainRoot(common, top string) string {
	common = filepath.Clean(common)
	if filepath.Base(common) == ".git" {
		root := filepath.Dir(common)
		if r, err := filepath.EvalSymlinks(root); err == nil {
			return r
		}
		return root
	}
	return top
}

func nearestExisting(dir string) string {
	for {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

var scpLike = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)

// NormalizeRemoteURL turns git remote URLs into a canonical, credential-free web
// form: "git@github.com:o/r.git" and "ssh://git@github.com/o/r.git" both become
// "https://github.com/o/r". Credentials, a trailing ".git" and trailing slashes are
// removed and the host is lower-cased. Local paths and file URLs are returned trimmed
// but otherwise unchanged.
func NormalizeRemoteURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		m := scpLike.FindStringSubmatch(s)
		if m == nil || strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") {
			return s
		}
		return "https://" + strings.ToLower(m[1]) + "/" + trimRepoPath(m[2])
	}
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	switch u.Scheme {
	case "file":
		return s
	case "ssh", "git", "git+ssh", "ssh+git":
		return "https://" + strings.ToLower(u.Hostname()) + "/" + trimRepoPath(u.Path)
	}
	host := strings.ToLower(u.Hostname())
	if p := u.Port(); p != "" {
		host += ":" + p
	}
	return u.Scheme + "://" + host + "/" + trimRepoPath(u.Path)
}

func trimRepoPath(p string) string {
	p = strings.Trim(p, "/")
	p = strings.TrimSuffix(p, ".git")
	return strings.TrimRight(p, "/")
}
