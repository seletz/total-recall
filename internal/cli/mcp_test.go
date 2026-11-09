package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/skill"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// TestMain builds the demo archive once for the package (the MCP tests
// share it); with TOTAL_RECALL_DEMO_DB set it uses the prebuilt one.
func TestMain(m *testing.M) { os.Exit(fixturedb.DemoMain(m)) }

// mcpSession drives `total-recall mcp` over its stdio in-process.
type mcpSession struct {
	t    *testing.T
	e    *testEnv
	args []string
	n    int
}

func (s *mcpSession) call(tool string, args any) (content []string, isError bool) {
	s.t.Helper()
	b, _ := json.Marshal(args)
	m := s.rpc("tools/call", fmt.Sprintf(`{"name":%q,"arguments":%s}`, tool, b))
	res, ok := m["result"].(map[string]any)
	if !ok {
		s.t.Fatalf("%s: no result: %v", tool, m)
	}
	for _, c := range res["content"].([]any) {
		content = append(content, c.(map[string]any)["text"].(string))
	}
	return content, res["isError"].(bool)
}

func (s *mcpSession) rpc(method, params string) map[string]any {
	s.t.Helper()
	s.n++
	msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, s.n, method, params)
	r := s.e.run(s.t, msg+"\n", append(s.args, "mcp")...)
	if r.code != 0 {
		s.t.Fatalf("mcp exit %d: %s", r.code, r.stderr)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(r.stdout)), &m); err != nil {
		s.t.Fatalf("stdout is not one JSON-RPC message: %v\n%s", err, r.stdout)
	}
	return m
}

func demoMCP(t *testing.T) (*mcpSession, *testEnv) {
	t.Helper()
	e := newTestEnv(t)
	_, db := fixturedb.Demo(t)
	return &mcpSession{t: t, e: e, args: []string{"--db", db}}, e
}

func TestMCPHandshakeAndTools(t *testing.T) {
	s, _ := demoMCP(t)
	m := s.rpc("initialize", `{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}`)
	res := m["result"].(map[string]any)
	if res["protocolVersion"] != "2025-06-18" || res["serverInfo"].(map[string]any)["name"] != "total-recall" {
		t.Errorf("initialize = %v", res)
	}
	list := s.rpc("tools/list", `{}`)["result"].(map[string]any)["tools"].([]any)
	var names []string
	for _, x := range list {
		tool := x.(map[string]any)
		names = append(names, tool["name"].(string))
		if tool["annotations"].(map[string]any)["readOnlyHint"] != true || tool["description"] == "" {
			t.Errorf("tool %v: %v", tool["name"], tool)
		}
	}
	if got, want := strings.Join(names, " "), "search sessions show stats memory status"; got != want {
		t.Errorf("tools = %s, want %s (stable interface: only append)", got, want)
	}
}

// TestMCPToolsMatchCLI: every tool returns exactly the --json output of its
// command against the demo archive.
func TestMCPToolsMatchCLI(t *testing.T) {
	s, e := demoMCP(t)
	sess := decode[[]struct {
		ID string `json:"id"`
	}](t, e.mustRun(t, append(s.args, "sessions", "--json", "--limit", "1")...))
	if len(sess) != 1 {
		t.Fatal("demo archive has no session")
	}
	id := sess[0].ID
	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
		cli  []string
	}{
		{"search", "search", map[string]any{"query": "docs", "limit": 3}, []string{"search", "docs", "--limit=3"}},
		{"search by session", "search", map[string]any{"query": "docs", "by_session": true, "actor": "human"}, []string{"search", "docs", "--by-session", "--actor=human", "--limit=20"}},
		{"search types array", "search", map[string]any{"query": "docs", "type": []string{"event", "session", "memory"}, "limit": 2}, []string{"search", "docs", "--type=event,session,memory", "--limit=2"}},
		{"search types string and project", "search", map[string]any{"query": "docs", "type": "session", "project": "kompass"}, []string{"search", "docs", "--type=session", "--project=kompass", "--limit=20"}},
		{"sessions", "sessions", map[string]any{"limit": 5}, []string{"sessions", "--limit=5"}},
		{"show", "show", map[string]any{"session": id, "limit": 3}, []string{"show", id, "--limit=3"}},
		{"stats", "stats", map[string]any{"since": "2026-10-01", "until": "2026-10-31", "workday": true}, []string{"stats", "--since=2026-10-01", "--until=2026-10-31", "--workday"}},
		{"stats blocks", "stats", map[string]any{"since": "2026-10-01", "until": "2026-10-31", "workday": true, "blocks": true}, []string{"stats", "--since=2026-10-01", "--until=2026-10-31", "--workday", "--blocks"}},
		{"memory", "memory", map[string]any{}, []string{"memory"}},
		{"status", "status", map[string]any{}, []string{"status"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, isErr := s.call(tc.tool, tc.args)
			if isErr {
				t.Fatalf("isError: %v", got)
			}
			r := e.run(t, "", append(append(s.args, tc.cli...), "--json")...)
			if r.code != 0 {
				t.Fatalf("cli exit %d: %s", r.code, r.stderr)
			}
			if len(got) != 1 {
				t.Fatalf("content = %d items", len(got))
			}
			if tc.tool == "status" {
				// raw_ratio is a sample and differs between two runs.
				a, b := decode[map[string]any](t, got[0]), decode[map[string]any](t, r.stdout)
				for _, k := range []string{"db", "schema_version", "sessions", "events", "migration_pending"} {
					if a[k] != b[k] {
						t.Errorf("status %s: tool %v, cli %v", k, a[k], b[k])
					}
				}
			} else if got[0] != strings.TrimSpace(r.stdout) {
				t.Errorf("tool output differs from CLI\n tool: %.300s\n  cli: %.300s", got[0], r.stdout)
			}
			if !json.Valid([]byte(got[0])) {
				t.Errorf("not JSON: %.200s", got[0])
			}
		})
	}
}

func TestMCPErrors(t *testing.T) {
	s, _ := demoMCP(t)
	for _, tc := range []struct {
		name, tool string
		args       map[string]any
		kind       string
	}{
		{"unknown session", "show", map[string]any{"session": "claude:nope-nope"}, "not_found"},
		{"bad time", "sessions", map[string]any{"since": "yesterday-ish"}, "usage"},
		{"bad fts", "search", map[string]any{"query": "AND AND (", "raw": true}, "sql"},
		{"bad type", "search", map[string]any{"query": "x", "type": []string{"attachment"}}, "usage"},
		{"type needs by_session event", "search", map[string]any{"query": "x", "type": "session", "by_session": true}, "usage"},
		{"missing query", "search", map[string]any{}, "usage"},
		{"flag injection", "search", map[string]any{"query": "x", "db": "/etc/passwd"}, "usage"},
		{"limit too large", "search", map[string]any{"query": "x", "limit": 100000}, "usage"},
		{"flag-like query is data", "search", map[string]any{"query": "--help"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, isErr := s.call(tc.tool, tc.args)
			if tc.kind == "" {
				if isErr {
					t.Fatalf("isError: %v", got)
				}
				return
			}
			if !isErr {
				t.Fatalf("no error: %v", got)
			}
			e := decode[ErrorJSON](t, got[0])
			if e.Error.Kind != tc.kind || e.Error.Message == "" {
				t.Errorf("error = %+v, want kind %s", e.Error, tc.kind)
			}
		})
	}
}

// TestMCPArchiveGone: a missing archive and an archive that appears later
// are handled per call; the server needs no restart.
func TestMCPArchiveGone(t *testing.T) {
	e := newTestEnv(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "later.db")
	s := &mcpSession{t: t, e: e, args: []string{"--db", db}}
	got, isErr := s.call("status", map[string]any{})
	if !isErr || decode[ErrorJSON](t, got[0]).Error.Kind != "db" {
		t.Fatalf("missing archive: %v isError=%v", got, isErr)
	}
	_, demoDB := fixturedb.Demo(t)
	b, err := os.ReadFile(demoDB)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, isErr = s.call("sessions", map[string]any{"limit": 1}); isErr {
		t.Fatalf("after the archive appeared: %v", got)
	}
}

// TestMCPOutdatedSchema: an archive with an older schema gives a db or sql
// error with the migrate hint (or works for queries that do not need the
// new tables); the call never crashes the server.
func TestMCPOutdatedSchema(t *testing.T) {
	e := newTestEnv(t)
	e.outdatedDB(t)
	s := &mcpSession{t: t, e: e, args: []string{"--db", e.db}}
	got, isErr := s.call("memory", map[string]any{})
	if !isErr {
		t.Fatalf("memory on an old archive: %v", got)
	}
	if msg := decode[ErrorJSON](t, got[0]).Error.Message; !strings.Contains(msg, "migrate") {
		t.Errorf("message lacks the migrate hint: %s", msg)
	}
	if _, isErr := s.call("status", map[string]any{}); isErr {
		t.Error("status must work on an old archive")
	}
}

// TestMCPBounded: a result over the byte limit is cut at an item boundary;
// the first content item stays valid CLI JSON and a second one says so.
func TestMCPPrint(t *testing.T) {
	e := newTestEnv(t)
	got := decode[map[string]map[string]map[string]any](t, e.mustRun(t, "mcp", "print", "--binary", "/opt/tr"))
	entry := got["mcpServers"]["total-recall"]
	if entry["type"] != "stdio" || entry["command"] != "/opt/tr" || fmt.Sprint(entry["args"]) != "[mcp]" {
		t.Errorf("entry = %v", entry)
	}
	if out := strings.TrimSpace(e.mustRun(t, "mcp", "print", "--command")); out != "claude mcp add --scope user total-recall -- total-recall mcp" {
		t.Errorf("--command = %q", out)
	}
	if r := e.run(t, "", "mcp", "print", "--harness", "x"); r.code == 0 {
		t.Error("unknown harness accepted")
	}
}

// TestMCPSkillDocumentsTools keeps SKILL.md honest: every tool and every
// argument appears in the MCP section.
func TestMCPSkillDocumentsTools(t *testing.T) {
	text := skill.Text()
	i := strings.Index(text, "## MCP server")
	j := strings.Index(text, "## Cost notes")
	if i < 0 || j < i {
		t.Fatal("SKILL.md has no MCP server section")
	}
	sec := text[i:j]
	for _, tool := range mcpTools() {
		if !strings.Contains(sec, "| `"+tool.Name+"` |") {
			t.Errorf("tool %s is not in the table", tool.Name)
		}
		for _, p := range tool.Params {
			if !strings.Contains(sec, "`"+p.Name+"`") && !strings.Contains(sec, "`"+p.Name+"*`") {
				t.Errorf("tool %s: argument %s is not documented", tool.Name, p.Name)
			}
		}
	}
}
