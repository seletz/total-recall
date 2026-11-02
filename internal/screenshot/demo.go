package screenshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/gitinfo"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/harness/claude"
	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
)

// DemoQuery is the search of the search and session screenshots.
const DemoQuery = "goreleaser cask"

// The invented project of the demo archive: a route-planning CLI called
// kompass, developed by a fictional person in a fictional home directory.
const (
	demoCwd      = "/home/mara/src/kompass"
	demoWorktree = "/home/mara/src/kompass__worktrees/gpx"
	demoRemote   = "https://git.invalid/mara/kompass"
)

// demoLoc is the time zone the demo conversations are written in.
var demoLoc = time.FixedZone("CET", 3600)

// demoSession is one invented conversation.
type demoSession struct {
	id    string
	cwd   string
	title string // custom title; empty means an AI title
	ai    string
	turns []demoTurn
}

// demoTurn is one exchange: the prompt, an optional tool call with its
// result, and the reply, timed from start.
type demoTurn struct {
	at      time.Time
	prompt  string
	tool    string // tool name; empty means no tool call
	input   string
	output  string
	reply   string
	replyAt time.Duration // delay of the reply after the prompt
}

func demoAt(month time.Month, day, hour, min int) time.Time {
	return time.Date(2026, month, day, hour, min, 0, 0, demoLoc)
}

// demoSessions are the conversations of the demo archive. They are invented
// developer chat about kompass in German and English; several mention
// goreleaser and the Homebrew cask, the DemoQuery.
var demoSessions = []demoSession{
	{
		id: "7d1e4c20-0001-4000-8000-000000000001", cwd: demoCwd, title: "Release v0.3.0",
		turns: []demoTurn{
			{at: demoAt(time.October, 31, 22, 14),
				prompt: "Lade den Homebrew-Cask mit goreleaser in den Tap hoch.",
				tool:   "Read", input: ".goreleaser.yml",
				output: "casks:\n  - name: kompass\n    repository:\n      owner: mara\n      name: homebrew-tap\n",
				reply:  "goreleaser skips the cask upload when the tap token is missing. I added a check that fails the release early instead, so a missing HOMEBREW_TAP_GITHUB_TOKEN no longer leaves a half-published release.",
				replyAt: 2 * time.Minute},
			{at: demoAt(time.October, 31, 22, 17),
				prompt: "Gut. Tagge v0.3.0 und starte das Release.",
				tool:   "Bash", input: "git tag -s v0.3.0 -m 'v0.3.0' && git push origin v0.3.0",
				output: "To git.invalid:mara/kompass\n * [new tag]         v0.3.0 -> v0.3.0\n",
				reply:  "Tagged v0.3.0 and pushed the tag. The release workflow is running; goreleaser builds the binaries and publishes the cask to the tap.",
				replyAt: 3 * time.Minute},
		},
	},
	{
		id: "7d1e4c20-0002-4000-8000-000000000002", cwd: demoCwd, ai: "CI pipeline",
		turns: []demoTurn{
			{at: demoAt(time.October, 28, 9, 2),
				prompt:  "can we build the cask in goreleaser and push it to the tap from CI?",
				reply:   "goreleaser needs HOMEBREW_TAP_GITHUB_TOKEN in the workflow secrets and a cask entry in .goreleaser.yml. I added both; the cask is published on every tag.",
				replyAt: 3 * time.Minute},
			{at: demoAt(time.October, 28, 9, 12),
				prompt: "Und der Lint-Job? Der schlägt seit gestern fehl.",
				tool:   "Bash", input: "golangci-lint run ./...",
				output: "route/cache.go:42:18: unused-parameter: parameter 'ctx' seems to be unused (revive)\n",
				reply:  "The lint job fails on a new golangci-lint rule: an unused parameter in route/cache.go. I removed the parameter and the job is green again.",
				replyAt: 4 * time.Minute},
		},
	},
	{
		id: "7d1e4c20-0003-4000-8000-000000000003", cwd: demoCwd, ai: "Packaging",
		turns: []demoTurn{
			{at: demoAt(time.October, 12, 15, 40),
				prompt:  "Übernimm die .goreleaser.yml aus dem Notizen-Projekt und passe sie für kompass an.",
				reply:   "copied .goreleaser.yml from the sister project kompass-web, renamed the binary to kompass and set the cask name. goreleaser builds darwin/arm64 and linux/amd64.",
				replyAt: 5 * time.Minute},
		},
	},
	{
		id: "7d1e4c20-0004-4000-8000-000000000004", cwd: demoCwd, ai: "Flaky route cache test",
		turns: []demoTurn{
			{at: demoAt(time.October, 20, 10, 15),
				prompt: "TestRouteCache schlägt gelegentlich fehl, bitte schau dir das an.",
				tool:   "Bash", input: "go test -run TestRouteCache -count=200 ./route/",
				output: "--- FAIL: TestRouteCache (0.01s)\n    cache_test.go:57: waypoints out of order\nFAIL\n",
				reply:  "The test depended on map iteration order. I sort the waypoints before comparing them; 200 runs pass now.",
				replyAt: 6 * time.Minute},
		},
	},
	{
		id: "7d1e4c20-0005-4000-8000-000000000005", cwd: demoCwd, ai: "Homebrew-Tap einrichten",
		turns: []demoTurn{
			{at: demoAt(time.October, 15, 11, 5),
				prompt: "Richte den Homebrew-Tap ein, damit goreleaser den Cask dorthin pusht.",
				tool:   "Bash", input: "gh repo create mara/homebrew-tap --public",
				output: "https://git.invalid/mara/homebrew-tap\n",
				reply:  "Created the tap repository and a deploy key. goreleaser publishes the cask there; users install it with brew install --cask mara/tap/kompass.",
				replyAt: 7 * time.Minute},
		},
	},
	{
		id: "7d1e4c20-0006-4000-8000-000000000006", cwd: demoCwd, title: "Signierte Releases",
		turns: []demoTurn{
			{at: demoAt(time.October, 22, 17, 30),
				prompt:  "Können wir die Release-Artefakte mit cosign signieren?",
				reply:   "Yes: goreleaser signs the checksums file with cosign after the build. The cask is unaffected, it still points at the signed archives.",
				replyAt: 4 * time.Minute},
		},
	},
	{
		id: "7d1e4c20-0007-4000-8000-000000000007", cwd: demoWorktree, title: "GPX-Import",
		turns: []demoTurn{
			{at: demoAt(time.October, 24, 16, 0),
				prompt: "Füge einen GPX-Import hinzu: Wegpunkte und Tracks, Streaming-Parser.",
				tool:   "Bash", input: "go test ./internal/gpx/...",
				output: "ok  \tkompass/internal/gpx\t0.412s\n",
				reply:  "Added internal/gpx with a streaming parser for waypoints and tracks, plus tests with a small synthetic file.",
				replyAt: 9 * time.Minute},
		},
	},
}

// demoGit is the git resolution of the demo directories; real git never runs.
var demoGit = map[string]gitinfo.Info{
	demoCwd:      {Dir: demoCwd, Toplevel: demoCwd, Root: demoCwd, RemoteURL: demoRemote, Branch: "main"},
	demoWorktree: {Dir: demoWorktree, Toplevel: demoWorktree, Root: demoCwd, RemoteURL: demoRemote, Branch: "gpx"},
}

// WriteDemo writes the demo transcripts as a Claude Code projects tree
// below dir and returns the projects directory. An existing tree is
// replaced, so transcripts of an earlier version do not linger.
func WriteDemo(dir string) (string, error) {
	projects := filepath.Join(dir, "projects")
	if err := os.RemoveAll(projects); err != nil {
		return "", fmt.Errorf("demo: %w", err)
	}
	n := 0
	next := func() string { n++; return fmt.Sprintf("d%04d", n) }
	for _, s := range demoSessions {
		var lines []testfixture.Line
		if s.title != "" {
			lines = append(lines, testfixture.CustomTitle(s.id, s.title))
		} else {
			lines = append(lines, testfixture.AITitle(s.id, s.ai))
		}
		for _, tr := range s.turns {
			lines = append(lines, testfixture.User(s.id, next(), stamp(tr.at), s.cwd, tr.prompt))
			if tr.tool != "" {
				use := next()
				lines = append(lines,
					testfixture.ToolUse(s.id, use, stamp(tr.at.Add(20*time.Second)), s.cwd, tr.tool, tr.input),
					testfixture.ToolResult(s.id, next(), stamp(tr.at.Add(25*time.Second)), s.cwd, use, tr.output))
			}
			lines = append(lines, testfixture.Assistant(s.id, next(), stamp(tr.at.Add(tr.replyAt)), s.cwd, tr.reply))
		}
		rel := filepath.Join(strings.NewReplacer("/", "-", "_", "-").Replace(s.cwd), s.id+".jsonl")
		p := filepath.Join(projects, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", fmt.Errorf("demo: %w", err)
		}
		if err := os.WriteFile(p, []byte(testfixture.Join(lines...)), 0o644); err != nil {
			return "", fmt.Errorf("demo: %w", err)
		}
	}
	return projects, nil
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z07:00") }

// BuildDemo writes the demo transcripts below dir and ingests them into
// dir/demo.db, which it returns. An existing demo.db is replaced.
func BuildDemo(ctx context.Context, dir string) (string, error) {
	projects, err := WriteDemo(dir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "demo.db")
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("demo: %w", err)
		}
	}
	st, err := store.Open(path)
	if err != nil {
		return "", fmt.Errorf("demo: %w", err)
	}
	defer func() { _ = st.Close() }()
	deps := ingest.Deps{
		Store:      st,
		Adapters:   []harness.Adapter{claude.New(projects)},
		StoreRaw:   true,
		ResolveGit: true,
		GitResolve: func(_ context.Context, cwd string) (gitinfo.Info, error) {
			if info, ok := demoGit[cwd]; ok {
				return info, nil
			}
			return gitinfo.Info{}, gitinfo.ErrNotRepo
		},
		LockDir: filepath.Join(dir, "locks"),
	}
	sum, err := ingest.All(ctx, deps)
	if err != nil {
		return "", fmt.Errorf("demo: ingest: %w", err)
	}
	if len(sum.Errors) > 0 {
		return "", fmt.Errorf("demo: ingest %s: %w", sum.Errors[0].Path, sum.Errors[0].Err)
	}
	return path, nil
}
