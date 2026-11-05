// Package testfixture builds small synthetic Claude Code transcripts for
// tests. It never contains real session data.
package testfixture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Line is one synthetic transcript record.
type Line map[string]any

// JSON encodes l as one JSONL line including the trailing newline.
func (l Line) JSON() string {
	b, err := json.Marshal(map[string]any(l))
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}

// User returns a typed user message.
func User(session, uuid, ts, cwd, text string) Line {
	return Line{"type": "user", "uuid": uuid, "timestamp": ts, "sessionId": session, "cwd": cwd,
		"gitBranch": "main", "version": "2.1.300", "origin": map[string]any{"kind": "human"},
		"message": map[string]any{"role": "user", "content": text}}
}

// Assistant returns an assistant text message.
func Assistant(session, uuid, ts, cwd, text string) Line {
	return Line{"type": "assistant", "uuid": uuid, "timestamp": ts, "sessionId": session, "cwd": cwd,
		"gitBranch": "main", "version": "2.1.300",
		"message": map[string]any{"id": "msg_" + uuid, "model": "test-model", "role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": text}}}}
}

// ToolUse returns an assistant tool_use record.
func ToolUse(session, uuid, ts, cwd, tool, command string) Line {
	return Line{"type": "assistant", "uuid": uuid, "timestamp": ts, "sessionId": session, "cwd": cwd,
		"message": map[string]any{"id": "msg_" + uuid, "model": "test-model", "role": "assistant",
			"content": []any{map[string]any{"type": "tool_use", "id": "toolu_" + uuid, "name": tool,
				"input": map[string]any{"command": command}}}}}
}

// CustomTitle returns a custom-title record.
func CustomTitle(session, title string) Line {
	return Line{"type": "custom-title", "customTitle": title, "sessionId": session}
}

// AITitle returns an ai-title record.
func AITitle(session, title string) Line {
	return Line{"type": "ai-title", "aiTitle": title, "sessionId": session}
}

// Account and organization UUIDs of the fixtures; SessionOwner uses the
// first account.
const (
	Account1      = "acc00000-0000-4000-8000-000000000001"
	Account2      = "acc00000-0000-4000-8000-000000000002"
	Organization1 = "org00000-0000-4000-8000-000000000001"
)

// SessionOwner returns a session-owner record (Claude Code 2.1.317+) for
// Account1 and Organization1; see SessionOwnerOf.
func SessionOwner(session string) Line {
	return SessionOwnerOf(session, Account1, Organization1)
}

// SessionOwnerOf returns a session-owner record (Claude Code 2.1.317+):
// account and organization of the session, repeated several times per
// transcript, without a timestamp. The adapter stores both on the session.
func SessionOwnerOf(session, account, organization string) Line {
	return Line{"type": "session-owner", "accountUuid": account, "organizationUuid": organization, "sessionId": session}
}

// ContinuedIn returns a continued-in record (Claude Code 2.1.318+): the
// session this one was continued in, written once at the end of a
// transcript. The adapter stores the link on the session.
func ContinuedIn(session, ts, continuedIn string) Line {
	return Line{"type": "continued-in", "timestamp": ts, "sessionId": session, "continuedInSessionId": continuedIn}
}

// HistorySuppression returns a history-suppression record: a cause and the
// account the suppression was vetoed against, with ts instead of timestamp.
// The adapter skips it; nothing of it is stored.
func HistorySuppression(session, ts string) Line {
	return Line{"type": "history-suppression", "sessionId": session, "cause": "chokepoint_veto",
		"vetoedAgainstAccountUuid": Account2, "ts": ts}
}

// Join concatenates lines as JSONL.
func Join(lines ...Line) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.JSON())
	}
	return b.String()
}

// WriteFile writes content to dir/rel, creating parent directories, and
// returns the absolute path.
func WriteFile(t testing.TB, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// AppendFile appends content to path.
func AppendFile(t testing.TB, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

// Session IDs of the Small fixture.
const (
	SmallSession1 = "11111111-1111-4111-8111-111111111111"
	SmallSession2 = "22222222-2222-4222-8222-222222222222"
)

// Small writes a small projects tree below dir: two sessions in two project
// directories, one with a subagent and a workflow journal. It returns the
// projects dir. Counts: session 1 has 2 human and 3 agent events (one is a
// tool_use), its subagent 1 human-ish (peer) and 1 agent event; session 2 has
// 1 human and 1 agent event.
func Small(t testing.TB, dir string) string {
	t.Helper()
	projects := filepath.Join(dir, "projects")
	s1, s2 := SmallSession1, SmallSession2
	WriteFile(t, projects, "-w-alpha/"+s1+".jsonl", Join(
		SessionOwner(s1),
		User(s1, "a1", "2026-10-05T08:00:00.000Z", "/w/alpha", "please list the files"),
		ToolUse(s1, "a2", "2026-10-05T08:00:01.000Z", "/w/alpha", "Bash", "ls"),
		Assistant(s1, "a3", "2026-10-05T08:00:02.000Z", "/w/alpha", "There are two files."),
		AITitle(s1, "List files"),
		SessionOwner(s1),
		User(s1, "a4", "2026-10-05T09:30:00.000Z", "/w/alpha", "thanks, now count the lines"),
		Assistant(s1, "a5", "2026-10-05T09:30:05.000Z", "/w/alpha", "Forty-two lines."),
	))
	WriteFile(t, projects, "-w-alpha/"+s1+"/subagents/agent-a01.jsonl", Join(
		Line{"type": "user", "uuid": "b1", "timestamp": "2026-10-05T08:00:10.000Z", "sessionId": s1, "agentId": "a01",
			"cwd": "/w/alpha", "message": map[string]any{"role": "user", "content": "Research task: count."}},
		Line{"type": "assistant", "uuid": "b2", "timestamp": "2026-10-05T08:00:20.000Z", "sessionId": s1, "agentId": "a01",
			"cwd": "/w/alpha", "message": map[string]any{"id": "msg_b2", "model": "test-model", "role": "assistant",
				"content": []any{map[string]any{"type": "text", "text": "Counted."}}}},
	))
	WriteFile(t, projects, "-w-alpha/"+s1+"/subagents/workflows/wf_1/journal.jsonl",
		Line{"type": "started", "agentId": "a02"}.JSON())
	WriteFile(t, projects, "-w-beta/"+s2+".jsonl", Join(
		CustomTitle(s2, "Beta work"),
		User(s2, "c1", "2026-10-06T14:00:00.000Z", "/w/beta", "write the release notes"),
		Assistant(s2, "c2", "2026-10-06T14:05:00.000Z", "/w/beta", "Release notes written."),
	))
	return projects
}

// ToolResult returns a user record carrying one tool_result block for the
// tool_use with the given uuid. The adapter maps it to an actor "tool" event.
func ToolResult(session, uuid, ts, cwd, toolUseUUID, output string) Line {
	return Line{"type": "user", "uuid": uuid, "timestamp": ts, "sessionId": session, "cwd": cwd,
		"gitBranch": "main", "version": "2.1.300",
		"message": map[string]any{"role": "user",
			"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_" + toolUseUUID, "content": output}}}}
}
