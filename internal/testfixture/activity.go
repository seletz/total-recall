package testfixture

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/gitinfo"
)

// Working directories and git identities of the Activity fixture. Gamma is
// not a git repository; the worktree resolves to alpha's root and remote.
const (
	ActivityAlpha         = "/w/alpha"
	ActivityAlphaWorktree = "/w/alpha__worktrees/feature"
	ActivityBeta          = "/w/beta"
	ActivityGamma         = "/w/gamma"
	ActivityAlphaRemote   = "https://example.test/o/alpha"
	ActivityBetaRemote    = "https://example.test/o/beta"
)

// Native session IDs of the Activity fixture (the store prefixes them with
// "claude:"). The subagent runs below ActivitySessionAlpha1.
const (
	ActivitySessionAlpha1    = "a1a1a1a1-0001-4000-8000-000000000001"
	ActivitySessionAlpha2    = "a1a1a1a1-0002-4000-8000-000000000002"
	ActivitySessionAlpha3    = "a1a1a1a1-0003-4000-8000-000000000003"
	ActivitySessionAlpha4    = "a1a1a1a1-0004-4000-8000-000000000004"
	ActivitySessionAlpha5    = "a1a1a1a1-0005-4000-8000-000000000005"
	ActivitySessionAlpha6    = "a1a1a1a1-0006-4000-8000-000000000006"
	ActivitySessionWorktree1 = "c1c1c1c1-0001-4000-8000-000000000001"
	ActivitySessionWorktree2 = "c1c1c1c1-0002-4000-8000-000000000002"
	ActivitySessionWorktree3 = "c1c1c1c1-0003-4000-8000-000000000003"
	ActivitySessionBeta1     = "b2b2b2b2-0001-4000-8000-000000000001"
	ActivitySessionBeta2     = "b2b2b2b2-0002-4000-8000-000000000002"
	ActivitySessionBeta3     = "b2b2b2b2-0003-4000-8000-000000000003"
	ActivitySessionBeta4     = "b2b2b2b2-0004-4000-8000-000000000004"
	ActivitySessionGamma1    = "d3d3d3d3-0001-4000-8000-000000000001"
	ActivitySessionGamma2    = "d3d3d3d3-0002-4000-8000-000000000002"
	ActivitySessionGamma3    = "d3d3d3d3-0003-4000-8000-000000000003"
	// ActivitySubagent is the native ID of the subagent session (agentId a01).
	ActivitySubagent = "agent-a01"
)

// ActivityLocation is the local time zone all Activity timestamps are
// designed in. The fixture lies entirely in December/January, when
// Europe/Berlin is UTC+1, so a fixed zone keeps tests independent of the
// zoneinfo database while matching the project's default time zone.
var ActivityLocation = time.FixedZone("CET", 3600)

// ActivityCount counts events by actor. Agent includes tool_use events
// (actor "agent"); Tool counts tool_result events (actor "tool"); Peer counts
// the parent's prompts in a subagent transcript.
type ActivityCount struct {
	Human, Agent, Tool, Peer int
}

// ActivityCell is the documented activity of one project in one ISO week
// (Week "YYYY-Www", local time ActivityLocation) or, with an empty Week,
// over the whole fixture. Project is the key `git_root, else cwd`;
// Sessions counts distinct top-level sessions with events in the cell.
type ActivityCell struct {
	Project, Week string
	Sessions      int
	ActivityCount
}

// ActivityWeeks are the per-project, per-ISO-week counts of the Activity
// fixture for top-level sessions (the subagent is in ActivitySubagentCount).
// Weeks without a row have no events for that project.
var ActivityWeeks = []ActivityCell{
	{ActivityAlpha, "2025-W49", 2, ActivityCount{Human: 3, Agent: 5, Tool: 2}},
	{ActivityAlpha, "2025-W50", 2, ActivityCount{Human: 2, Agent: 3, Tool: 1}},
	{ActivityAlpha, "2025-W51", 1, ActivityCount{Human: 3, Agent: 5, Tool: 2}},
	{ActivityAlpha, "2025-W52", 1, ActivityCount{Human: 1, Agent: 1, Tool: 0}},
	{ActivityAlpha, "2026-W01", 1, ActivityCount{Human: 2, Agent: 3, Tool: 1}},
	{ActivityAlpha, "2026-W02", 2, ActivityCount{Human: 3, Agent: 5, Tool: 2}},
	{ActivityBeta, "2025-W49", 1, ActivityCount{Human: 1, Agent: 2, Tool: 1}},
	{ActivityBeta, "2025-W50", 1, ActivityCount{Human: 2, Agent: 3, Tool: 1}},
	{ActivityBeta, "2025-W52", 1, ActivityCount{Human: 1, Agent: 2, Tool: 1}},
	{ActivityBeta, "2026-W02", 1, ActivityCount{Human: 1, Agent: 1, Tool: 0}},
	{ActivityGamma, "2025-W49", 1, ActivityCount{Human: 1, Agent: 1, Tool: 0}},
	{ActivityGamma, "2025-W51", 1, ActivityCount{Human: 1, Agent: 2, Tool: 1}},
	{ActivityGamma, "2026-W01", 1, ActivityCount{Human: 1, Agent: 2, Tool: 1}},
}

// ActivityTotals are the per-project totals of ActivityWeeks; Sessions counts
// all top-level sessions of the project (alpha: 6 main + 3 worktree).
var ActivityTotals = []ActivityCell{
	{ActivityAlpha, "", 9, ActivityCount{Human: 14, Agent: 22, Tool: 8}},
	{ActivityBeta, "", 4, ActivityCount{Human: 5, Agent: 8, Tool: 3}},
	{ActivityGamma, "", 3, ActivityCount{Human: 3, Agent: 5, Tool: 2}},
}

// ActivitySubagentCount is the activity of the one subagent session
// (ActivitySubagent, parent ActivitySessionAlpha1, cwd ActivityAlpha, 2025-W49).
var ActivitySubagentCount = ActivityCount{Peer: 1, Agent: 2, Tool: 1}

// ActivityGit is the deterministic git resolution for the Activity fixture,
// to be used instead of real git (ingest.Deps.GitResolve): alpha and its
// worktree share root and remote, beta has its own, gamma is not a repository
// (no entry, resolve with gitinfo.ErrNotRepo).
func ActivityGit() map[string]gitinfo.Info {
	return map[string]gitinfo.Info{
		ActivityAlpha:         {Dir: ActivityAlpha, Toplevel: ActivityAlpha, Root: ActivityAlpha, RemoteURL: ActivityAlphaRemote, Branch: "main"},
		ActivityAlphaWorktree: {Dir: ActivityAlphaWorktree, Toplevel: ActivityAlphaWorktree, Root: ActivityAlpha, RemoteURL: ActivityAlphaRemote, Branch: "feature"},
		ActivityBeta:          {Dir: ActivityBeta, Toplevel: ActivityBeta, Root: ActivityBeta, RemoteURL: ActivityBetaRemote, Branch: "main"},
	}
}

// Activity writes a synthetic projects tree below dir covering six ISO
// weeks (2025-W49 .. 2026-W02, 2025-12-01 .. 2026-01-11 in ActivityLocation)
// and returns the projects dir. It is meant for day/week/month aggregation
// and the dimensions project, agent (main/subagent) and directory.
//
// Layout: three projects alpha (/w/alpha, git), beta (/w/beta, git) and
// gamma (/w/gamma, no git), plus the worktree /w/alpha__worktrees/feature of
// alpha's repository and one subagent below alpha session 1. Every session
// has one to three turns; a turn is a human prompt and an agent reply,
// optionally with one tool_use (actor agent) and its tool_result (actor
// tool) in between. Events fall on all seven weekdays and on many hours.
//
// Sessions (local time), events human/agent/tool per turn in brackets:
//
//	2025-W49  alpha  A1  Mon 12-01 09:15 [1/2/1], 09:40 [1/1/0]; subagent a01 09:16 peer 1, agent 2, tool 1
//	          alpha  A2  Wed 12-03 14:05 [1/2/1]
//	          beta   B1  Thu 12-04 20:30 [1/2/1]  (custom title "Beta release")
//	          gamma  G1  Sat 12-06 11:00 [1/1/0]
//	2025-W50  alpha  W1  Mon 12-08 00:30 [1/2/1]  worktree; 2025-12-07T23:30Z, so Sunday/W49 in UTC
//	          beta   B2  Tue 12-09 10:00 [1/2/1], 10:20 [1/1/0]
//	          alpha  A3  Fri 12-12 16:45 [1/1/0]
//	2025-W51  gamma  G2  Tue 12-16 08:00 [1/2/1]
//	          alpha  A4  Thu 12-18 13:20 [1/2/1], 13:35 [1/2/1], 13:50 [1/1/0]
//	2025-W52  beta   B3  Tue 12-23 09:30 [1/2/1]
//	          alpha  W2  Sat 12-27 22:10 [1/1/0]  worktree
//	2026-W01  alpha  A5  Wed 12-31 23:40 [1/2/1], Thu 01-01 00:05 [1/1/0]  spans the year boundary
//	          gamma  G3  Fri 01-02 15:00 [1/2/1]
//	2026-W02  beta   B4  Mon 01-05 08:45 [1/1/0]
//	          alpha  A6  Thu 01-08 17:30 [1/2/1], 17:55 [1/1/0]
//	          alpha  W3  Sun 01-11 19:00 [1/2/1]  worktree
//
// Per project and ISO week (sessions, human/agent/tool), top-level sessions
// only; see ActivityWeeks and ActivityTotals for the same numbers as data:
//
//	week      alpha          beta           gamma
//	2025-W49  2  3/5/2       1  1/2/1       1  1/1/0
//	2025-W50  2  2/3/1       1  2/3/1       -
//	2025-W51  1  3/5/2       -              1  1/2/1
//	2025-W52  1  1/1/0       1  1/2/1       -
//	2026-W01  1  2/3/1       -              1  1/2/1
//	2026-W02  2  3/5/2       1  1/1/0       -
//	total     9  14/22/8     4  5/8/3       3  3/5/2
//
// Per month (local): December 2025 alpha 10/16/6, beta 4/7/3, gamma 2/3/1;
// January 2026 alpha 4/6/2, beta 1/1/0, gamma 1/2/1. Only A5's second turn
// moves between months; in UTC it still belongs to December.
//
// Directory dimension: /w/alpha 6 sessions 11/17/6, /w/alpha__worktrees/feature
// 3 sessions 3/5/2, /w/beta 4 sessions 5/8/3, /w/gamma 3 sessions 3/5/2.
func Activity(t testing.TB, dir string) string {
	t.Helper()
	projects := filepath.Join(dir, "projects")
	w := activityWriter{t: t, projects: projects}

	// 2025-W49
	w.session(ActivityAlpha, ActivitySessionAlpha1, []Line{SessionOwner(ActivitySessionAlpha1)},
		turn{at(2025, 12, 1, 9, 15), true, "list the open issues", "gh issue list", "3 open issues", "Three issues are open."},
		turn{at(2025, 12, 1, 9, 40), false, "summarise the oldest one", "", "", "The oldest asks for log rotation."},
	)
	w.subagent(ActivityAlpha, ActivitySessionAlpha1, "a01", at(2025, 12, 1, 9, 16),
		"Research task: count the test files.", "find . -name '*_test.go' | wc -l", "12", "There are twelve test files.")
	w.session(ActivityAlpha, ActivitySessionAlpha2, []Line{SessionOwner(ActivitySessionAlpha2)},
		turn{at(2025, 12, 3, 14, 5), true, "run the tests", "go test ./...", "ok", "All tests pass."})
	w.session(ActivityBeta, ActivitySessionBeta1, []Line{CustomTitle(ActivitySessionBeta1, "Beta release"), SessionOwnerOf(ActivitySessionBeta1, Account2, Organization1)},
		turn{at(2025, 12, 4, 20, 30), true, "tag the release", "git tag v0.2.0", "", "Tagged v0.2.0."})
	w.session(ActivityGamma, ActivitySessionGamma1, nil,
		turn{at(2025, 12, 6, 11, 0), false, "what is in this folder?", "", "", "Notes and a few drafts."})
	w.continued(ActivityGamma, ActivitySessionGamma1, ActivitySessionGamma2, at(2025, 12, 16, 7, 59))

	// 2025-W50
	w.session(ActivityAlphaWorktree, ActivitySessionWorktree1, nil,
		turn{at(2025, 12, 8, 0, 30), true, "rebase onto develop", "git rebase develop", "Successfully rebased", "Rebased without conflicts."})
	w.session(ActivityBeta, ActivitySessionBeta2, nil,
		turn{at(2025, 12, 9, 10, 0), true, "show the failing job", "gh run view", "lint failed", "The lint job failed."},
		turn{at(2025, 12, 9, 10, 20), false, "how do I fix it?", "", "", "Run the formatter and push again."},
	)
	w.session(ActivityAlpha, ActivitySessionAlpha3, nil,
		turn{at(2025, 12, 12, 16, 45), false, "explain the config loader", "", "", "It merges defaults, file and flags."})

	// 2025-W51
	w.session(ActivityGamma, ActivitySessionGamma2, nil,
		turn{at(2025, 12, 16, 8, 0), true, "count the words", "wc -w notes.md", "1200", "About twelve hundred words."})
	w.session(ActivityAlpha, ActivitySessionAlpha4, nil,
		turn{at(2025, 12, 18, 13, 20), true, "build it", "go build ./...", "", "Build succeeded."},
		turn{at(2025, 12, 18, 13, 35), true, "vet it", "go vet ./...", "", "No findings."},
		turn{at(2025, 12, 18, 13, 50), false, "thanks", "", "", "You are welcome."},
	)

	// 2025-W52
	w.session(ActivityBeta, ActivitySessionBeta3, nil,
		turn{at(2025, 12, 23, 9, 30), true, "check the disk usage", "du -sh .", "40M", "Forty megabytes."})
	w.session(ActivityAlphaWorktree, ActivitySessionWorktree2, nil,
		turn{at(2025, 12, 27, 22, 10), false, "what is left on this branch?", "", "", "Only the docs update."})

	// 2026-W01
	w.session(ActivityAlpha, ActivitySessionAlpha5, nil,
		turn{at(2025, 12, 31, 23, 40), true, "show the year's commit count", "git rev-list --count HEAD", "420", "Four hundred twenty commits."},
		turn{at(2026, 1, 1, 0, 5), false, "happy new year", "", "", "Happy new year."},
	)
	w.session(ActivityGamma, ActivitySessionGamma3, nil,
		turn{at(2026, 1, 2, 15, 0), true, "list the drafts", "ls drafts", "a.md b.md", "Two drafts."})

	// 2026-W02
	w.session(ActivityBeta, ActivitySessionBeta4, nil,
		turn{at(2026, 1, 5, 8, 45), false, "plan the week", "", "", "Start with the release notes."})
	w.session(ActivityAlpha, ActivitySessionAlpha6, nil,
		turn{at(2026, 1, 8, 17, 30), true, "run the linter", "golangci-lint run", "", "Clean."},
		turn{at(2026, 1, 8, 17, 55), false, "commit it", "", "", "Committed."},
	)
	w.session(ActivityAlphaWorktree, ActivitySessionWorktree3, nil,
		turn{at(2026, 1, 11, 19, 0), true, "push the branch", "git push", "done", "Pushed."})

	return projects
}

// at returns the UTC timestamp string of a local time in ActivityLocation.
func at(year int, month time.Month, day, hour, min int) time.Time {
	return time.Date(year, month, day, hour, min, 0, 0, ActivityLocation)
}

func stamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// turn is one exchange: a human prompt, optionally a tool_use with its
// tool_result, then the agent's reply.
type turn struct {
	ts       time.Time
	withTool bool
	prompt   string
	command  string
	output   string
	reply    string
}

type activityWriter struct {
	t        testing.TB
	projects string
	n        int
}

// projectDir mirrors Claude Code's directory name for a cwd.
func projectDir(cwd string) string {
	return strings.NewReplacer("/", "-", "_", "-").Replace(cwd)
}

// next returns a fresh record uuid.
func (w *activityWriter) next() string {
	w.n++
	return fmt.Sprintf("e%04d", w.n)
}

func (w *activityWriter) lines(session, cwd string, turns []turn) []Line {
	var out []Line
	for _, tr := range turns {
		out = append(out, User(session, w.next(), stamp(tr.ts), cwd, tr.prompt))
		if tr.withTool {
			use := w.next()
			out = append(out,
				ToolUse(session, use, stamp(tr.ts.Add(2*time.Second)), cwd, "Bash", tr.command),
				ToolResult(session, w.next(), stamp(tr.ts.Add(3*time.Second)), cwd, use, tr.output))
		}
		out = append(out, Assistant(session, w.next(), stamp(tr.ts.Add(5*time.Second)), cwd, tr.reply))
	}
	return out
}

// continued appends a continued-in record to the transcript of session:
// it was continued in next at ts (Gamma1 → Gamma2 in the fixture).
func (w *activityWriter) continued(cwd, session, next string, ts time.Time) {
	w.t.Helper()
	AppendFile(w.t, filepath.Join(w.projects, projectDir(cwd), session+".jsonl"), ContinuedIn(session, stamp(ts), next).JSON())
}

func (w *activityWriter) session(cwd, session string, head []Line, turns ...turn) {
	w.t.Helper()
	lines := append(head, w.lines(session, cwd, turns)...)
	WriteFile(w.t, w.projects, projectDir(cwd)+"/"+session+".jsonl", Join(lines...))
}

// subagent writes one subagent transcript below parent: a prompt from the
// parent (actor peer), one tool_use with result, and the agent's reply.
func (w *activityWriter) subagent(cwd, parent, agentID string, ts time.Time, prompt, command, output, reply string) {
	w.t.Helper()
	use := w.next()
	lines := []Line{
		Line{"type": "user", "uuid": w.next(), "timestamp": stamp(ts), "sessionId": parent, "agentId": agentID,
			"cwd": cwd, "message": map[string]any{"role": "user", "content": prompt}},
		Line{"type": "assistant", "uuid": use, "timestamp": stamp(ts.Add(2 * time.Second)), "sessionId": parent, "agentId": agentID,
			"cwd": cwd, "message": map[string]any{"id": "msg_" + use, "model": "test-model", "role": "assistant",
				"content": []any{map[string]any{"type": "tool_use", "id": "toolu_" + use, "name": "Bash",
					"input": map[string]any{"command": command}}}}},
		Line{"type": "user", "uuid": w.next(), "timestamp": stamp(ts.Add(3 * time.Second)), "sessionId": parent, "agentId": agentID,
			"cwd": cwd, "message": map[string]any{"role": "user",
				"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_" + use, "content": output}}}},
		Line{"type": "assistant", "uuid": w.next(), "timestamp": stamp(ts.Add(5 * time.Second)), "sessionId": parent, "agentId": agentID,
			"cwd": cwd, "message": map[string]any{"id": "msg_" + w.next(), "model": "test-model", "role": "assistant",
				"content": []any{map[string]any{"type": "text", "text": reply}}}},
	}
	WriteFile(w.t, w.projects, projectDir(cwd)+"/"+parent+"/subagents/agent-"+agentID+".jsonl", Join(lines...))
}
