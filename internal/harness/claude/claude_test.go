package claude

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/model"
)

const (
	sid     = "11111111-1111-4111-8111-111111111111"
	fixture = "testdata/projects/-tmp-demo/" + sid + ".jsonl"
)

// parseFile runs Parse on path from offset and collects all records.
func parseFile(t *testing.T, a *Adapter, path string, offset int64) ([]model.Record, int64) {
	t.Helper()
	var recs []model.Record
	off, err := a.Parse(context.Background(), path, offset, func(r model.Record) error {
		recs = append(recs, r)
		return nil
	})
	if err != nil {
		t.Fatalf("Parse(%s, %d): %v", path, offset, err)
	}
	return recs, off
}

// writeFile writes content to a fresh file at rel (relative to a temp dir) and returns its path.
func writeFile(t *testing.T, rel, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// parseLine parses a single JSONL line in a main-session file and returns all events.
func parseLine(t *testing.T, a *Adapter, line string) []model.Event {
	t.Helper()
	p := writeFile(t, "proj/"+sid+".jsonl", line+"\n")
	recs, _ := parseFile(t, a, p, 0)
	var evs []model.Event
	for _, r := range recs {
		evs = append(evs, r.Events...)
	}
	return evs
}

// userLine builds a synthetic user record; extra is a JSON fragment of additional fields (may be empty).
func userLine(extra, content string) string {
	if extra != "" {
		extra = "," + extra
	}
	return fmt.Sprintf(`{"type":"user","uuid":"u-1","timestamp":"2026-10-02T10:00:00.000Z","sessionId":%q,"cwd":"/tmp/x"%s,"message":{"role":"user","content":%s}}`, sid, extra, content)
}

func TestNameAndRegister(t *testing.T) {
	a := New("testdata/projects")
	if a.Name() != "claude" {
		t.Fatalf("Name() = %q", a.Name())
	}
	Register("testdata/projects")
	got, ok := harness.Get("claude")
	if !ok || got.Name() != "claude" {
		t.Fatalf("adapter not registered")
	}
}

func TestDefaultProjectsDir(t *testing.T) {
	tests := []struct {
		name, configDir, home, want string
	}{
		{"config dir", "/cfg", "/home/u", "/cfg/projects"},
		{"home", "", "/home/u", "/home/u/.claude/projects"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLAUDE_CONFIG_DIR", tt.configDir)
			t.Setenv("HOME", tt.home)
			got, err := DefaultProjectsDir()
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSources(t *testing.T) {
	a := New("testdata/projects")
	got, err := a.Sources()
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs("testdata/projects")
	want := []string{
		abs + "/-tmp-demo/" + sid + ".jsonl",
		abs + "/-tmp-demo/" + sid + "/subagents/agent-a0000000000000001.jsonl",
		abs + "/-tmp-demo/" + sid + "/subagents/workflows/wf_0001/agent-a0000000000000002.jsonl",
		abs + "/-tmp-demo/" + sid + "/subagents/workflows/wf_0001/journal.jsonl",
		abs + "/-tmp-other/22222222-2222-4222-8222-222222222222.jsonl",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sources() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSourcesMissingDir(t *testing.T) {
	a := New(filepath.Join(t.TempDir(), "nope"))
	got, err := a.Sources()
	if err != nil || len(got) != 0 {
		t.Fatalf("Sources() = %v, %v; want empty, nil", got, err)
	}
}

func TestParseFixture(t *testing.T) {
	a := New("testdata/projects")
	recs, off := parseFile(t, a, fixture, 0)
	fi, _ := os.Stat(fixture)
	if off != fi.Size() {
		t.Errorf("offset = %d, want file size %d", off, fi.Size())
	}

	type row struct {
		ID, Kind, Role, Actor, Text, Tool, Model string
		In, Out                                  int64
	}
	var got []row
	var sess []model.Session
	for _, r := range recs {
		sess = append(sess, r.Session)
		for _, e := range r.Events {
			if e.SessionID != "claude:"+sid {
				t.Errorf("event %s session = %q", e.ID, e.SessionID)
			}
			if e.Raw == "" || strings.HasSuffix(e.Raw, "\n") {
				t.Errorf("event %s raw = %q", e.ID, e.Raw)
			}
			if !strings.HasSuffix(e.SourcePath, fixture) {
				t.Errorf("event %s source path = %q", e.ID, e.SourcePath)
			}
			got = append(got, row{e.ID, string(e.Kind), e.Role, string(e.Actor), e.Text, e.ToolName, e.Model, e.TokensIn, e.TokensOut})
		}
	}
	want := []row{
		{"claude:aaaaaaaa-0000-4000-8000-000000000001", "message", "user", "human", "please list the files", "", "", 0, 0},
		{"claude:aaaaaaaa-0000-4000-8000-000000000002#0", "tool_use", "assistant", "agent", "ls -la", "Bash", "test-model-1", 113, 20},
		{"claude:aaaaaaaa-0000-4000-8000-000000000003#0", "tool_result", "tool", "tool", "a.txt\nb.txt", "", "", 0, 0},
		{"claude:aaaaaaaa-0000-4000-8000-000000000005#0", "message", "assistant", "agent", "There are two files.", "", "test-model-1", 1, 5},
		{"claude:aaaaaaaa-0000-4000-8000-000000000006", "system", "system", "automation", "turn_duration", "", "", 0, 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events =\n%+v\nwant\n%+v", got, want)
	}

	// 9 ingested records (permission-mode, attachment and history-suppression skipped).
	if len(recs) != 9 {
		t.Fatalf("records = %d, want 9", len(recs))
	}
	if owner := sess[0]; owner.ID != "claude:"+sid || owner.AccountID != "acc00000-0000-4000-8000-000000000001" ||
		owner.OrganizationID != "org00000-0000-4000-8000-000000000001" || !owner.StartedAt.IsZero() || len(recs[0].Events) != 0 {
		t.Errorf("session-owner record = %+v", recs[0])
	}
	first := sess[1]
	if first.ID != "claude:"+sid || first.Harness != "claude" || first.NativeID != sid ||
		first.HarnessVersion != "2.1.300" || first.Cwd != "/tmp/demo" || first.GitBranch != "main" ||
		!first.StartedAt.Equal(time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)) || !first.EndedAt.Equal(first.StartedAt) {
		t.Errorf("first session = %+v", first)
	}
	if sess[2].Model != "test-model-1" {
		t.Errorf("assistant session model = %q", sess[2].Model)
	}
	if sess[6].Name != "demo session" || sess[6].ID != "claude:"+sid || len(recs[6].Events) != 0 {
		t.Errorf("custom-title record = %+v", recs[6])
	}
	if sess[7].AgentName != "demo-agent" || sess[7].Name != "" || len(recs[7].Events) != 0 {
		t.Errorf("agent-name record = %+v", recs[7])
	}
	// continued-in links the session, without events and without touching
	// its times (the record is written when the session is resumed).
	if cont := sess[8]; cont.ID != "claude:"+sid || cont.ContinuedIn != "claude:33333333-3333-4333-8333-333333333333" ||
		!cont.StartedAt.IsZero() || !cont.EndedAt.IsZero() || len(recs[8].Events) != 0 {
		t.Errorf("continued-in record = %+v", recs[8])
	}
}

// TestSessionOwnerAndContinuedIn: the meta records of Claude Code 2.1.317+
// set session fields and never produce events; incomplete ones set nothing.
func TestSessionOwnerAndContinuedIn(t *testing.T) {
	tests := []struct {
		name string
		line string
		want model.Session
	}{
		{"owner", `{"type":"session-owner","accountUuid":"acc-1","organizationUuid":"org-1","sessionId":"` + sid + `"}`,
			model.Session{AccountID: "acc-1", OrganizationID: "org-1"}},
		{"owner without org", `{"type":"session-owner","accountUuid":"acc-1","sessionId":"` + sid + `"}`,
			model.Session{AccountID: "acc-1"}},
		{"continued", `{"type":"continued-in","timestamp":"2026-11-04T07:00:00.000Z","sessionId":"` + sid + `","continuedInSessionId":"bbbbbbbb-0000-4000-8000-000000000002"}`,
			model.Session{ContinuedIn: "claude:bbbbbbbb-0000-4000-8000-000000000002"}},
		{"continued without target", `{"type":"continued-in","timestamp":"2026-11-04T07:00:00.000Z","sessionId":"` + sid + `"}`,
			model.Session{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := writeFile(t, "p/"+sid+".jsonl", tt.line+"\n")
			recs, _ := parseFile(t, New(""), p, 0)
			if len(recs) != 1 || len(recs[0].Events) != 0 {
				t.Fatalf("records = %+v", recs)
			}
			tt.want.ID, tt.want.Harness, tt.want.NativeID = "claude:"+sid, "claude", sid
			if got := recs[0].Session; got != tt.want {
				t.Errorf("session =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestParseSummaryLegacy(t *testing.T) {
	a := New("testdata/projects")
	recs, _ := parseFile(t, a, "testdata/projects/-tmp-other/22222222-2222-4222-8222-222222222222.jsonl", 0)
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	s := recs[0].Session
	if s.ID != "claude:22222222-2222-4222-8222-222222222222" || s.AITitle != "Legacy summary title" || s.Name != "" || len(recs[0].Events) != 0 {
		t.Errorf("summary record = %+v", recs[0])
	}
}

func TestParseSubagents(t *testing.T) {
	tests := []struct {
		name, path, wantID, wantText string
	}{
		{"subagent", "testdata/projects/-tmp-demo/" + sid + "/subagents/agent-a0000000000000001.jsonl", "claude:agent-a0000000000000001", "Research task: count the files."},
		{"workflow subagent", "testdata/projects/-tmp-demo/" + sid + "/subagents/workflows/wf_0001/agent-a0000000000000002.jsonl", "claude:agent-a0000000000000002", "Workflow step: say hi."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recs, _ := parseFile(t, New("testdata/projects"), tt.path, 0)
			if len(recs) == 0 {
				t.Fatal("no records")
			}
			for _, r := range recs {
				if r.Session.ID != tt.wantID || r.Session.ParentID != "claude:"+sid || r.Session.NativeID != strings.TrimPrefix(tt.wantID, "claude:") {
					t.Errorf("session = %+v", r.Session)
				}
				for _, e := range r.Events {
					if e.SessionID != tt.wantID {
						t.Errorf("event session = %q", e.SessionID)
					}
				}
			}
			e := recs[0].Events[0]
			if e.Actor != model.ActorPeer || e.Text != tt.wantText {
				t.Errorf("first subagent event = %+v, want peer prompt", e)
			}
		})
	}
}

func TestSubagentSessionWithoutAgentID(t *testing.T) {
	p := writeFile(t, "proj/"+sid+"/subagents/agent-afff.jsonl",
		`{"type":"assistant","uuid":"x-1","timestamp":"2026-10-02T10:00:00Z","sessionId":"`+sid+`","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`+"\n")
	recs, _ := parseFile(t, New(""), p, 0)
	if len(recs) != 1 || recs[0].Session.ID != "claude:agent-afff" || recs[0].Session.ParentID != "claude:"+sid {
		t.Fatalf("records = %+v", recs)
	}
}

func TestClassifyUser(t *testing.T) {
	tests := []struct {
		name      string
		extra     string
		content   string
		wantKind  model.Kind
		wantRole  string
		wantActor model.Actor
		wantText  string
	}{
		{"typed string", `"origin":{"kind":"human"},"promptSource":"typed"`, `"hello there"`, model.KindMessage, "user", model.ActorHuman, "hello there"},
		{"no origin defaults human", ``, `"plain"`, model.KindMessage, "user", model.ActorHuman, "plain"},
		{"text blocks", ``, `[{"type":"text","text":"part one"}]`, model.KindMessage, "user", model.ActorHuman, "part one"},
		{"peer another session", `"isMeta":true,"origin":{"kind":"peer","from":"uds:/tmp/x.sock"}`, `"Another Claude session sent a message: hi"`, model.KindMessage, "user", model.ActorPeer, "Another Claude session sent a message: hi"},
		{"peer origin only", `"isMeta":true,"origin":{"kind":"peer"}`, `"Note:\n<agent-message from=\"x\">hi</agent-message>"`, model.KindMessage, "user", model.ActorPeer, "Note:\n<agent-message from=\"x\">hi</agent-message>"},
		{"agent-message prefix", ``, `"<agent-message from=\"x\">hi</agent-message>"`, model.KindMessage, "user", model.ActorPeer, "<agent-message from=\"x\">hi</agent-message>"},
		{"cross-session", `"isMeta":true`, `"[Cross-session idle notice] peer is idle"`, model.KindMessage, "user", model.ActorPeer, "[Cross-session idle notice] peer is idle"},
		{"task notification", `"origin":{"kind":"task-notification"}`, `"<task-notification>\n<task-id>t1</task-id>\n</task-notification>"`, model.KindMessage, "user", model.ActorAutomation, "<task-notification>\n<task-id>t1</task-id>\n</task-notification>"},
		{"task notification origin only", `"origin":{"kind":"task-notification"}`, `"background job done"`, model.KindMessage, "user", model.ActorAutomation, "background job done"},
		{"isMeta", `"isMeta":true`, `"Look at the thing again."`, model.KindMessage, "user", model.ActorAutomation, "Look at the thing again."},
		{"scheduled task", `"scheduledTaskId":"st-1"`, `"Hourly check"`, model.KindMessage, "user", model.ActorAutomation, "Hourly check"},
		{"command", `"origin":{"kind":"human"}`, `"<command-name>/model</command-name>"`, model.KindMessage, "user", model.ActorAutomation, "<command-name>/model</command-name>"},
		{"local command", ``, `"<local-command-stdout>ok</local-command-stdout>"`, model.KindMessage, "user", model.ActorAutomation, "<local-command-stdout>ok</local-command-stdout>"},
		{"continued", `"isCompactSummary":true`, `"This session is being continued from a previous conversation."`, model.KindMessage, "user", model.ActorAutomation, "This session is being continued from a previous conversation."},
		{"skill base dir", ``, `[{"type":"text","text":"Base directory for this skill: /x"}]`, model.KindMessage, "user", model.ActorAutomation, "Base directory for this skill: /x"},
		{"system reminder", ``, `"<system-reminder>\nbe nice\n</system-reminder>"`, model.KindMessage, "user", model.ActorAutomation, "<system-reminder>\nbe nice\n</system-reminder>"},
		{"interrupted", ``, `[{"type":"text","text":"[Request interrupted by user]"}]`, model.KindMessage, "user", model.ActorAutomation, "[Request interrupted by user]"},
		{"bash stdout", ``, `"<bash-stdout>done</bash-stdout><bash-stderr></bash-stderr>"`, model.KindMessage, "user", model.ActorAutomation, "<bash-stdout>done</bash-stdout><bash-stderr></bash-stderr>"},
		{"bash input", ``, `"<bash-input>git pull</bash-input>"`, model.KindMessage, "user", model.ActorHuman, "<bash-input>git pull</bash-input>"},
		{"pasted", `"origin":{"kind":"human"}`, `"<pasted_content id=\"1\">blob</pasted_content> see above"`, model.KindMessage, "user", model.ActorHuman, "<pasted_content id=\"1\">blob</pasted_content> see above"},
		{"leading whitespace", ``, `"\n  <system-reminder>x</system-reminder>"`, model.KindMessage, "user", model.ActorAutomation, "\n  <system-reminder>x</system-reminder>"},
		{"tool result", ``, `[{"type":"tool_result","tool_use_id":"t1","content":"out"}]`, model.KindToolResult, "tool", model.ActorTool, "out"},
		{"tool result blocks", ``, `[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"l1"},{"type":"image","source":{}},{"type":"text","text":"l2"}]}]`, model.KindToolResult, "tool", model.ActorTool, "l1\n[image]\nl2"},
		{"tool result error", ``, `[{"type":"tool_result","tool_use_id":"t1","content":"boom","is_error":true}]`, model.KindToolResult, "tool", model.ActorTool, "[error] boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evs := parseLine(t, New(""), userLine(tt.extra, tt.content))
			if len(evs) != 1 {
				t.Fatalf("events = %d (%+v), want 1", len(evs), evs)
			}
			e := evs[0]
			if e.Kind != tt.wantKind || e.Role != tt.wantRole || e.Actor != tt.wantActor || e.Text != tt.wantText {
				t.Errorf("got kind=%s role=%s actor=%s text=%q, want %s %s %s %q", e.Kind, e.Role, e.Actor, e.Text, tt.wantKind, tt.wantRole, tt.wantActor, tt.wantText)
			}
		})
	}
}

func TestClassifyExtraPrefixes(t *testing.T) {
	a := New("", WithPeerPrefixes("Coordinator:"), WithAutomationPrefixes("Hourly ", "Weekend watch"))
	tests := []struct {
		text string
		want model.Actor
	}{
		{"Coordinator: please rebase", model.ActorPeer},
		{"Hourly #12 scan", model.ActorAutomation},
		{"Weekend watch of things", model.ActorAutomation},
		{"hello", model.ActorHuman},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			evs := parseLine(t, a, userLine(`"origin":{"kind":"human"}`, fmt.Sprintf("%q", tt.text)))
			if len(evs) != 1 || evs[0].Actor != tt.want {
				t.Errorf("actor = %+v, want %s", evs, tt.want)
			}
		})
	}
}

func TestMixedUserBlocks(t *testing.T) {
	evs := parseLine(t, New(""), userLine("", `[{"type":"tool_result","tool_use_id":"t1","content":"r"},{"type":"image","source":{}},{"type":"text","text":"<system-reminder>x</system-reminder>"},{"type":"smell"}]`))
	if len(evs) != 3 {
		t.Fatalf("events = %+v", evs)
	}
	if evs[0].ID != "claude:u-1#0" || evs[0].Actor != model.ActorTool {
		t.Errorf("ev0 = %+v", evs[0])
	}
	if evs[1].ID != "claude:u-1#1" || evs[1].Actor != model.ActorHuman || evs[1].Kind != model.KindMessage || evs[1].Text != "[image]" {
		t.Errorf("ev1 = %+v", evs[1])
	}
	if evs[2].ID != "claude:u-1#2" || evs[2].Actor != model.ActorAutomation || evs[2].Kind != model.KindMessage {
		t.Errorf("ev2 = %+v", evs[2])
	}
}

func TestToolResultTruncation(t *testing.T) {
	long := strings.Repeat("ä", 10) // 20 bytes
	tests := []struct {
		name string
		max  int
		want string
	}{
		{"fits", 100, long},
		{"cut on rune boundary", 9, "ääää" + truncMark},
		{"exact", 20, long},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evs := parseLine(t, New("", WithToolResultMaxBytes(tt.max)), userLine("", fmt.Sprintf(`[{"type":"tool_result","tool_use_id":"t","content":%q}]`, long)))
			if evs[0].Text != tt.want {
				t.Errorf("text = %q, want %q", evs[0].Text, tt.want)
			}
		})
	}
	if New("").toolResultMax != 4096 {
		t.Errorf("default tool result max = %d", New("").toolResultMax)
	}
}

func TestAssistant(t *testing.T) {
	line := func(id, uuid, content, usage string) string {
		return fmt.Sprintf(`{"type":"assistant","uuid":%q,"timestamp":"2026-10-02T10:00:00Z","sessionId":%q,"message":{"model":"m-1","id":%q,"role":"assistant","content":%s,"usage":%s}}`, uuid, sid, id, content, usage)
	}
	big := strings.Repeat("x", 3000)
	tests := []struct {
		name  string
		lines []string
		want  []model.Event
	}{
		{
			name: "text, thinking skipped, tool_use with summary",
			lines: []string{line("msg_1", "a-1", `[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"Answer."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/x/y.go","limit":10}}]`,
				`{"input_tokens":1,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":4}`)},
			want: []model.Event{
				{ID: "claude:a-1#1", Kind: model.KindMessage, Role: "assistant", Actor: model.ActorAgent, Text: "Answer.", Model: "m-1", TokensIn: 6, TokensOut: 4},
				{ID: "claude:a-1#2", Kind: model.KindToolUse, Role: "assistant", Actor: model.ActorAgent, Text: "/x/y.go", ToolName: "Read", Model: "m-1"},
			},
		},
		{
			name: "usage counted once per message id",
			lines: []string{
				line("msg_2", "a-2", `[{"type":"text","text":"one"}]`, `{"input_tokens":5,"output_tokens":7}`),
				line("msg_2", "a-3", `[{"type":"tool_use","id":"t2","name":"mcp__srv__do","input":{"a":1,"b":"two"}}]`, `{"input_tokens":5,"output_tokens":7}`),
			},
			want: []model.Event{
				{ID: "claude:a-2#0", Kind: model.KindMessage, Role: "assistant", Actor: model.ActorAgent, Text: "one", Model: "m-1", TokensIn: 5, TokensOut: 7},
				{ID: "claude:a-3#0", Kind: model.KindToolUse, Role: "assistant", Actor: model.ActorAgent, Text: `{"a":1,"b":"two"}`, ToolName: "mcp__srv__do", Model: "m-1"},
			},
		},
		{
			name:  "long tool input truncated",
			lines: []string{line("msg_3", "a-4", `[{"type":"tool_use","id":"t3","name":"Bash","input":{"command":"`+big+`"}}]`, `null`)},
			want: []model.Event{
				{ID: "claude:a-4#0", Kind: model.KindToolUse, Role: "assistant", Actor: model.ActorAgent, Text: big[:1024] + truncMark, ToolName: "Bash", Model: "m-1"},
			},
		},
		{
			name:  "synthetic model ignored",
			lines: []string{strings.Replace(line("msg_4", "a-5", `[{"type":"text","text":"API Error"}]`, `null`), `"m-1"`, `"<synthetic>"`, 1)},
			want: []model.Event{
				{ID: "claude:a-5#0", Kind: model.KindMessage, Role: "assistant", Actor: model.ActorAgent, Text: "API Error"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := writeFile(t, "p/"+sid+".jsonl", strings.Join(tt.lines, "\n")+"\n")
			recs, _ := parseFile(t, New(""), p, 0)
			var got []model.Event
			for _, r := range recs {
				for _, e := range r.Events {
					e.SessionID, e.TS, e.SourcePath, e.SourceOffset, e.Raw = "", time.Time{}, "", 0, ""
					got = append(got, e)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("events =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestSystemRecord(t *testing.T) {
	tests := []struct {
		name, line, wantText string
	}{
		{"content", `{"type":"system","subtype":"informational","content":"Heads up","level":"notice","uuid":"s-1","timestamp":"2026-10-02T10:00:00Z","sessionId":"` + sid + `"}`, "Heads up"},
		{"no content uses subtype", `{"type":"system","subtype":"stop_hook_summary","uuid":"s-1","timestamp":"2026-10-02T10:00:00Z","sessionId":"` + sid + `"}`, "stop_hook_summary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evs := parseLine(t, New(""), tt.line)
			if len(evs) != 1 {
				t.Fatalf("events = %+v", evs)
			}
			e := evs[0]
			if e.ID != "claude:s-1" || e.Kind != model.KindSystem || e.Role != "system" || e.Actor != model.ActorAutomation || e.Text != tt.wantText {
				t.Errorf("event = %+v", e)
			}
		})
	}
}

func TestParseAITitle(t *testing.T) {
	p := writeFile(t, "p/"+sid+".jsonl", `{"type":"ai-title","aiTitle":"Generated title","sessionId":"`+sid+`"}`+"\n"+
		`{"type":"custom-title","customTitle":"Mine","sessionId":"`+sid+`"}`+"\n")
	recs, _ := parseFile(t, New(""), p, 0)
	if len(recs) != 2 {
		t.Fatalf("records = %d", len(recs))
	}
	if s := recs[0].Session; s.AITitle != "Generated title" || s.Name != "" || len(recs[0].Events) != 0 {
		t.Errorf("ai-title record = %+v", recs[0])
	}
	if s := recs[1].Session; s.Name != "Mine" || s.AITitle != "" {
		t.Errorf("custom-title record = %+v", recs[1])
	}
}

func TestSkippedRecordTypes(t *testing.T) {
	types := []string{"attachment", "api-request", "api-request-blob", "api-request-shape", "bridge-session", "last-prompt",
		"mode", "atis-latch", "permission-mode", "pr-link", "queue-operation", "file-history-snapshot", "file-history-delta",
		"cost-state", "tools-used", "started", "result", "history-suppression", "something-new"}
	for _, typ := range types {
		t.Run(typ, func(t *testing.T) {
			p := writeFile(t, "p/"+sid+".jsonl", `{"type":"`+typ+`","uuid":"z","sessionId":"`+sid+`","message":{"role":"user","content":"x"}}`+"\n")
			recs, off := parseFile(t, New(""), p, 0)
			if len(recs) != 0 || off == 0 {
				t.Errorf("records = %+v, offset = %d", recs, off)
			}
		})
	}
	t.Run("configured skip", func(t *testing.T) {
		p := writeFile(t, "p/"+sid+".jsonl", `{"type":"system","subtype":"turn_duration","uuid":"z","sessionId":"`+sid+`"}`+"\n")
		recs, _ := parseFile(t, New("", WithSkipRecordTypes("system")), p, 0)
		if len(recs) != 0 {
			t.Errorf("records = %+v", recs)
		}
	})
}

func TestParseOffsets(t *testing.T) {
	l1 := userLine("", `"first"`)
	l2 := strings.Replace(userLine("", `"second"`), "u-1", "u-2", 1)
	huge := strings.Replace(userLine("", fmt.Sprintf("%q", strings.Repeat("y", 3<<20))), "u-1", "u-3", 1)
	tests := []struct {
		name       string
		content    string
		offset     int64
		wantTexts  []string
		wantOffset int64
		wantBad    int64
	}{
		{"complete lines", l1 + "\n" + l2 + "\n", 0, []string{"first", "second"}, int64(len(l1) + len(l2) + 2), 0},
		{"trailing partial line left", l1 + "\n" + l2[:20], 0, []string{"first"}, int64(len(l1) + 1), 0},
		{"resume from offset", l1 + "\n" + l2 + "\n", int64(len(l1) + 1), []string{"second"}, int64(len(l1) + len(l2) + 2), 0},
		{"malformed skipped", l1 + "\n{not json\n" + l2 + "\n", 0, []string{"first", "second"}, int64(len(l1) + len(l2) + 12), 1},
		{"blank lines and CRLF", l1 + "\r\n\n" + l2 + "\n", 0, []string{"first", "second"}, int64(len(l1) + len(l2) + 4), 0},
		{"huge line", huge + "\n", 0, []string{strings.Repeat("y", 3<<20)}, int64(len(huge) + 1), 0},
		{"offset at end", l1 + "\n", int64(len(l1) + 1), nil, int64(len(l1) + 1), 0},
		{"empty file", "", 0, nil, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := writeFile(t, "p/"+sid+".jsonl", tt.content)
			a := New("")
			recs, off := parseFile(t, a, p, tt.offset)
			var texts []string
			for _, r := range recs {
				for _, e := range r.Events {
					texts = append(texts, e.Text)
					if e.SourceOffset < tt.offset || e.SourceOffset >= off {
						t.Errorf("source offset %d outside [%d,%d)", e.SourceOffset, tt.offset, off)
					}
				}
			}
			if !reflect.DeepEqual(texts, tt.wantTexts) {
				t.Errorf("texts = %d items, want %d", len(texts), len(tt.wantTexts))
			}
			if off != tt.wantOffset {
				t.Errorf("offset = %d, want %d", off, tt.wantOffset)
			}
			if got := a.Stats().Malformed; got != tt.wantBad {
				t.Errorf("malformed = %d, want %d", got, tt.wantBad)
			}
		})
	}
}

func TestParseSourceOffsetAndFallbackID(t *testing.T) {
	l1 := userLine("", `"a"`)
	l2 := `{"type":"user","timestamp":"2026-10-02T10:00:00Z","sessionId":"` + sid + `","message":{"role":"user","content":"no uuid"}}`
	p := writeFile(t, "p/"+sid+".jsonl", l1+"\n"+l2+"\n")
	recs, _ := parseFile(t, New(""), p, 0)
	e := recs[1].Events[0]
	if e.SourceOffset != int64(len(l1)+1) {
		t.Errorf("source offset = %d", e.SourceOffset)
	}
	if !strings.HasPrefix(e.ID, "claude:") || !strings.HasSuffix(e.ID, fmt.Sprintf(":%d", len(l1)+1)) || len(e.ID) != len("claude:")+40+1+len(fmt.Sprint(len(l1)+1)) {
		t.Errorf("fallback id = %q", e.ID)
	}
}

func TestParseEmitErrorAndCancel(t *testing.T) {
	l1 := userLine("", `"a"`)
	l2 := strings.Replace(userLine("", `"b"`), "u-1", "u-2", 1)
	p := writeFile(t, "p/"+sid+".jsonl", l1+"\n"+l2+"\n")
	boom := errors.New("boom")
	n := 0
	off, err := New("").Parse(context.Background(), p, 0, func(model.Record) error {
		n++
		if n == 2 {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) || off != int64(len(l1)+1) {
		t.Errorf("emit error: off=%d err=%v", off, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	off, err = New("").Parse(ctx, p, 0, func(model.Record) error { return nil })
	if !errors.Is(err, context.Canceled) || off != 0 {
		t.Errorf("cancel: off=%d err=%v", off, err)
	}

	if _, err := New("").Parse(context.Background(), filepath.Join(t.TempDir(), "missing.jsonl"), 0, func(model.Record) error { return nil }); err == nil {
		t.Error("missing file: want error")
	}
}

// TestRawIsTheFullLine: since #180 raw is the whole transcript line, not an
// extract; the fields the store keeps in columns stay in it too.
func TestRawIsTheFullLine(t *testing.T) {
	line := userLine(`"gitBranch":"main","version":"2.1.0","requestId":"req_1"`, `"hi"`)
	evs := parseLine(t, New(""), line)
	if len(evs) != 1 || evs[0].Raw != line {
		t.Errorf("raw = %q, want the line %q", evs[0].Raw, line)
	}
}

func TestAttachments(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nnot really a picture")
	jpg := bytes.Repeat([]byte{0xff, 0xd8, 0xff, 0xe0}, 300) // 1200 bytes
	b64 := base64.StdEncoding.EncodeToString
	hash := blobref.Hash
	a := New("")

	t.Run("pasted image", func(t *testing.T) {
		line := userLine("", `[{"type":"text","text":"look at this"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`+b64(png)+`"}}]`)
		evs := parseLine(t, a, line)
		if len(evs) != 2 {
			t.Fatalf("events = %+v", evs)
		}
		if evs[0].ID != "claude:u-1#0" || evs[0].Text != "look at this" || len(evs[0].Attachments) != 0 {
			t.Errorf("text event = %+v", evs[0])
		}
		img := evs[1]
		if img.ID != "claude:u-1#1" || img.Kind != model.KindMessage || img.Role != "user" || img.Actor != model.ActorHuman || img.Text != "[image image/png 28 B]" {
			t.Errorf("image event = %+v", img)
		}
		if len(img.Attachments) != 1 || img.Attachments[0].SHA256 != hash(png) || img.Attachments[0].MediaType != "image/png" || !bytes.Equal(img.Attachments[0].Data, png) {
			t.Errorf("image attachments = %+v", img.Attachments)
		}
		for _, e := range evs {
			if strings.Contains(e.Raw, b64(png)) || !strings.Contains(e.Raw, `"data":"blob:sha256:`+hash(png)+`"`) {
				t.Errorf("raw of %s still carries the data or no reference: %s", e.ID, e.Raw)
			}
			if !strings.Contains(e.Raw, `"cwd":"/tmp/x"`) {
				t.Errorf("raw of %s is not the full line: %s", e.ID, e.Raw)
			}
		}
	})

	t.Run("tool result with image twice", func(t *testing.T) {
		// The Read tool returns the image as a tool_result block and Claude
		// Code repeats it in toolUseResult.file: one blob, two positions.
		line := `{"type":"user","uuid":"u-2","timestamp":"2026-10-02T10:00:01.000Z","sessionId":"` + sid + `","cwd":"/tmp/x",` +
			`"message":{"role":"user","content":[{"tool_use_id":"t1","type":"tool_result","content":[{"type":"text","text":"here"},{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"` + b64(jpg) + `"}}]}]},` +
			`"toolUseResult":{"type":"image","file":{"base64":"` + b64(jpg) + `","type":"image/jpeg","originalSize":1200,"dimensions":{"originalWidth":2,"originalHeight":2}}}}`
		evs := parseLine(t, a, line)
		if len(evs) != 1 {
			t.Fatalf("events = %+v", evs)
		}
		e := evs[0]
		if e.Kind != model.KindToolResult || e.Text != "here\n[image image/jpeg 1.2 KiB]" {
			t.Errorf("event = %+v", e)
		}
		if len(e.Attachments) != 2 || e.Attachments[0].SHA256 != hash(jpg) || e.Attachments[1].SHA256 != hash(jpg) || e.Attachments[1].MediaType != "image/jpeg" {
			t.Errorf("attachments = %+v", e.Attachments)
		}
		if strings.Contains(e.Raw, b64(jpg)) || strings.Count(e.Raw, "blob:sha256:"+hash(jpg)) != 2 || !strings.Contains(e.Raw, `"originalSize":1200`) {
			t.Errorf("raw = %s", e.Raw)
		}
	})

	t.Run("document and unknown source", func(t *testing.T) {
		pdf := []byte("%PDF-1.4 fake")
		line := userLine("", `[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"`+b64(pdf)+`"}},{"type":"image","source":{"type":"url","url":"https://x.invalid/a.png"}},{"type":"image","source":{}}]`)
		evs := parseLine(t, a, line)
		if len(evs) != 3 {
			t.Fatalf("events = %+v", evs)
		}
		if evs[0].Text != "[document application/pdf 13 B]" || len(evs[0].Attachments) != 1 || evs[0].Attachments[0].MediaType != "application/pdf" {
			t.Errorf("document event = %+v", evs[0])
		}
		if evs[1].Text != "[image]" || len(evs[1].Attachments) != 0 || evs[2].Text != "[image]" || len(evs[2].Attachments) != 0 {
			t.Errorf("image events = %+v, %+v", evs[1], evs[2])
		}
		if strings.Contains(evs[0].Raw, b64(pdf)) || !strings.Contains(evs[0].Raw, `"url":"https://x.invalid/a.png"`) {
			t.Errorf("raw = %s", evs[0].Raw)
		}
	})

	t.Run("subagent paste is still classified", func(t *testing.T) {
		p := writeFile(t, "proj/"+sid+"/subagents/agent-a1.jsonl", userLine("", `[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`+b64(png)+`"}}]`)+"\n")
		recs, _ := parseFile(t, a, p, 0)
		if len(recs) != 1 || len(recs[0].Events) != 1 || recs[0].Events[0].Actor != model.ActorPeer || len(recs[0].Events[0].Attachments) != 1 {
			t.Errorf("records = %+v", recs)
		}
	})

	t.Run("line that is not JSON after all", func(t *testing.T) {
		evs := parseLine(t, a, `{"type":"user","base64":`)
		if len(evs) != 0 {
			t.Errorf("events = %+v", evs)
		}
	})
}

func TestSizeLabel(t *testing.T) {
	for _, tt := range []struct {
		n    int
		want string
	}{{0, "0 B"}, {29, "29 B"}, {1023, "1023 B"}, {1024, "1.0 KiB"}, {1200, "1.2 KiB"}, {1 << 20, "1.0 MiB"}, {3*(1<<20) + 400000, "3.4 MiB"}} {
		if got := sizeLabel(tt.n); got != tt.want {
			t.Errorf("sizeLabel(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
