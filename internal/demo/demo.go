// Package demo builds the synthetic demo archive of total-recall: the
// invented Claude Code sessions of a fictional developer, written as a
// projects tree and ingested into a database. It is the shared large
// fixture of the project: the screenshots of the docs (internal/screenshot)
// and the tests that need a populated archive
// (internal/testfixture/fixturedb) read it, and `mise run demo:db` builds
// it as a file.
//
// Everything is invented: the person (mara, home /home/mara), the projects,
// the conversations and the remote host git.invalid. The generator is
// deterministic: a fixed seed, fixed dates and a fixed clock yield the same
// archive on every run and every machine. The activity covers May to
// October 2026 with workdays around the core hours, evenings, nights past
// midnight, weekends, idle public holidays of Baden-Württemberg, a worked
// one (Fronleichnam) and a two-week vacation in August; the week of
// 2026-06-01 is laid out by hand for the activity screenshots. Several
// sessions in October are written out as conversations about the release
// of kompass, so a search for Query finds them. Sessions from September on
// carry the account they ran on (AccountTeam for the projects of the
// organization, AccountPersonal for the rest), and some sessions are
// continued in the next one (continued-in record), among them two of the
// October conversations.
package demo

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata" // Location must load without a zoneinfo database

	"github.com/seletz/total-recall/internal/gitinfo"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/harness/claude"
	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
)

// The fictional developer and where the archive says the transcripts live.
const (
	// Home is the home directory of the fictional developer.
	Home = "/home/mara"
	// Projects is the Claude Code projects directory the archive names as
	// the origin of its transcripts. The generator writes them to a
	// temporary directory, whose path never ends up in the archive.
	Projects = Home + "/.claude/projects"
	// Query is the search of the search and session screenshots; the
	// conversations of October answer it, the invented filler never does.
	Query = "goreleaser cask"
)

// Location is the time zone the activity is designed in.
var Location = mustLocation("Europe/Berlin")

// Dates of the archive: the first and the last day with activity and the
// clock a reader of the archive should use, the morning after the last
// conversation.
var (
	First = time.Date(2026, 5, 4, 0, 0, 0, 0, Location)
	Last  = time.Date(2026, 10, 31, 0, 0, 0, 0, Location)
	Now   = time.Date(2026, 11, 2, 10, 0, 0, 0, Location)
)

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// Summary is what Build wrote.
type Summary struct {
	Sessions, Events int
}

// Build writes the demo archive to path, replacing an existing one. The
// transcripts are written to a temporary directory, ingested with real
// git never running (see Git) and removed again; the archive names
// Projects as their origin.
func Build(ctx context.Context, path string) (Summary, error) {
	tmp, err := os.MkdirTemp("", "total-recall-demo-")
	if err != nil {
		return Summary{}, fmt.Errorf("demo: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	projects, err := Write(tmp)
	if err != nil {
		return Summary{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Summary{}, fmt.Errorf("demo: %w", err)
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return Summary{}, fmt.Errorf("demo: %w", err)
		}
	}
	// The archive says it was last read at Now, so a status rendered
	// against it does not depend on when it was built.
	clock := Now // the memory replay moves it back in time
	st, err := store.Open(path, store.WithClock(func() time.Time { return clock }))
	if err != nil {
		return Summary{}, fmt.Errorf("demo: %w", err)
	}
	defer func() { _ = st.Close() }()
	git := Git()
	deps := ingest.Deps{
		Store:       st,
		Adapters:    []harness.Adapter{claude.New(projects)},
		StoreRaw:    true,
		StoreBlobs:  true,
		StoreMemory: true,
		ResolveGit:  true,
		GitResolve: func(_ context.Context, cwd string) (gitinfo.Info, error) {
			if info, ok := git[cwd]; ok {
				return info, nil
			}
			return gitinfo.Info{}, gitinfo.ErrNotRepo
		},
		LockDir: filepath.Join(tmp, "locks"),
	}
	if err := replayMemory(ctx, projects, deps, &clock); err != nil {
		return Summary{}, err
	}
	if err := writeMemory(projects); err != nil {
		return Summary{}, err
	}
	clock = Now
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
	return relocate(ctx, path, projects, Projects)
}

// relocate rewrites the transcript paths of the archive at dbPath from the
// directory they were read from to where the demo says they live, and
// counts what the archive holds.
func relocate(ctx context.Context, dbPath, from, to string) (Summary, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		return Summary{}, fmt.Errorf("demo: %w", err)
	}
	defer func() { _ = db.Close() }()
	// Events reference their source by id (schema v5): the path lives in
	// sources only.
	if _, err := db.ExecContext(ctx, "UPDATE sources SET path = ? || substr(path, ?) WHERE substr(path, 1, ?) = ?",
		to, len(from)+1, len(from), from); err != nil {
		return Summary{}, fmt.Errorf("demo: relocate: %w", err)
	}
	// The page layout after the ingest depends on the length of the
	// temporary path; a vacuum lays the file out from its content alone,
	// so size and page count are the same in every build (the status of
	// the TUI shows them).
	if _, err := db.ExecContext(ctx, "VACUUM"); err != nil {
		return Summary{}, fmt.Errorf("demo: vacuum: %w", err)
	}
	var s Summary
	if err := db.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM sessions), (SELECT count(*) FROM events)").Scan(&s.Sessions, &s.Events); err != nil {
		return Summary{}, fmt.Errorf("demo: %w", err)
	}
	return s, nil
}

// Write writes the demo transcripts as a Claude Code projects tree below
// dir and returns the projects directory. An existing tree is replaced,
// so transcripts of an earlier version do not linger.
func Write(dir string) (string, error) {
	projects := filepath.Join(dir, "projects")
	if err := os.RemoveAll(projects); err != nil {
		return "", fmt.Errorf("demo: %w", err)
	}
	for _, s := range plan() {
		if err := writeSession(projects, s); err != nil {
			return "", err
		}
	}
	if err := writeMemory(projects); err != nil {
		return "", err
	}
	return projects, nil
}

// writeSession writes one session and its subagents.
func writeSession(projects string, s session) error {
	var lines []testfixture.Line
	if s.title != "" {
		lines = append(lines, testfixture.CustomTitle(s.id, s.title))
	} else {
		lines = append(lines, testfixture.AITitle(s.id, s.ai))
	}
	if s.account != "" {
		lines = append(lines, testfixture.SessionOwnerOf(s.id, s.account, s.organization))
	}
	lines = append(lines, s.lines(s.id, "")...)
	if s.continuedIn != "" {
		last := s.turns[len(s.turns)-1]
		lines = append(lines, testfixture.ContinuedIn(s.id, stamp(last.at.Add(last.replyAt+time.Hour)), s.continuedIn))
	}
	dir := filepath.Join(projects, projectDir(s.cwd))
	if err := writeLines(filepath.Join(dir, s.id+".jsonl"), lines); err != nil {
		return err
	}
	for _, a := range s.subagents {
		var lines []testfixture.Line
		lines = append(lines, testfixture.Line{"type": "agent-name", "agentName": a.name, "sessionId": s.id, "agentId": a.id})
		lines = append(lines, a.session.lines(s.id, a.id)...)
		if err := writeLines(filepath.Join(dir, s.id, "subagents", "agent-"+a.id+".jsonl"), lines); err != nil {
			return err
		}
	}
	return nil
}

func writeLines(path string, lines []testfixture.Line) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("demo: %w", err)
	}
	if err := os.WriteFile(path, []byte(testfixture.Join(lines...)), 0o644); err != nil {
		return fmt.Errorf("demo: %w", err)
	}
	return nil
}

// projectDir mirrors Claude Code's directory name for a cwd.
func projectDir(cwd string) string {
	return strings.NewReplacer("/", "-", "_", "-").Replace(cwd)
}

// lines renders the turns of a session as transcript records of session
// sid; a non-empty agentID marks them as a subagent's.
func (s session) lines(sid, agentID string) []testfixture.Line {
	var out []testfixture.Line
	add := func(l testfixture.Line) {
		if agentID != "" {
			l["agentId"] = agentID
			delete(l, "origin") // the prompt comes from the parent session
		}
		out = append(out, l)
	}
	for _, tr := range s.turns {
		user := testfixture.User(sid, tr.ids[0], stamp(tr.at), s.cwd, tr.prompt)
		if len(tr.attached) > 0 {
			user = testfixture.UserWithAttachments(sid, tr.ids[0], stamp(tr.at), s.cwd, tr.prompt, tr.attached...)
		}
		if tr.remote {
			user["entrypoint"] = "remote"
		}
		add(user)
		t := tr.at.Add(20 * time.Second)
		for i, tool := range tr.tools {
			use := tr.ids[1+2*i]
			add(testfixture.ToolUse(sid, use, stamp(t), s.cwd, tool.name, tool.input))
			add(testfixture.ToolResult(sid, tr.ids[2+2*i], stamp(t.Add(5*time.Second)), s.cwd, use, tool.output))
			t = t.Add(25 * time.Second)
		}
		add(testfixture.Assistant(sid, tr.ids[len(tr.ids)-1], stamp(tr.at.Add(tr.replyAt)), s.cwd, tr.reply))
	}
	return out
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z07:00") }
