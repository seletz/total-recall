package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func testServer(run Runner) *Server {
	return &Server{
		Name: "total-recall", Version: "test", Run: run, MaxBytes: 200,
		Tools: []Tool{{
			Name: "search", Description: "d", Command: []string{"search"}, FixedFlags: []string{"--json"},
			Params: []Param{
				{Name: "query", Type: "string", Required: true, Positional: true},
				{Name: "actor", Type: "string", Enum: []string{"human", "agent"}},
				{Name: "limit", Type: "integer", Max: 50},
				{Name: "by_session", Type: "boolean"},
				{Name: "type", Type: "list", Enum: []string{"event", "session"}},
			},
			Defaults: map[string]int{"limit": 20},
		}, {
			Name: "show", Command: []string{"show"}, FixedFlags: []string{"--json"}, Offset: "offset",
			Params: []Param{{Name: "session", Type: "string", Required: true, Positional: true}, {Name: "offset", Type: "integer"}},
		}},
	}
}

func rpc(t *testing.T, s *Server, msg string) map[string]any {
	t.Helper()
	out := s.Handle(context.Background(), []byte(msg))
	if out == nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("response is not JSON: %v: %s", err, out)
	}
	if m["jsonrpc"] != "2.0" {
		t.Fatalf("jsonrpc = %v", m["jsonrpc"])
	}
	return m
}

func TestInitializeNegotiatesVersion(t *testing.T) {
	s := testServer(nil)
	for _, tc := range []struct{ client, want string }{
		{"2025-06-18", "2025-06-18"},
		{"2024-11-05", "2024-11-05"},
		{"2099-01-01", ProtocolVersions[0]},
		{"", ProtocolVersions[0]},
	} {
		m := rpc(t, s, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`, tc.client))
		res := m["result"].(map[string]any)
		if res["protocolVersion"] != tc.want {
			t.Errorf("client %q: version %v, want %s", tc.client, res["protocolVersion"], tc.want)
		}
		if _, ok := res["capabilities"].(map[string]any)["tools"]; !ok {
			t.Errorf("no tools capability: %v", res)
		}
		if res["serverInfo"].(map[string]any)["name"] != "total-recall" {
			t.Errorf("serverInfo = %v", res["serverInfo"])
		}
		if m["id"] != float64(1) {
			t.Errorf("id = %v", m["id"])
		}
	}
}

func TestJSONRPCErrors(t *testing.T) {
	s := testServer(nil)
	for _, tc := range []struct {
		name, msg string
		code      float64
		id        any
	}{
		{"parse", `{not json`, -32700, nil},
		{"batch", `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, -32600, nil},
		{"not 2.0", `{"jsonrpc":"1.0","id":7,"method":"ping"}`, -32600, float64(7)},
		{"unknown method", `{"jsonrpc":"2.0","id":"a","method":"resources/list"}`, -32601, "a"},
		{"unknown tool", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sql"}}`, -32602, float64(2)},
		{"no name", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{}}`, -32602, float64(3)},
	} {
		m := rpc(t, s, tc.msg)
		e, ok := m["error"].(map[string]any)
		if !ok || e["code"] != tc.code {
			t.Errorf("%s: %v, want code %v", tc.name, m, tc.code)
			continue
		}
		if m["id"] != tc.id {
			t.Errorf("%s: id = %v, want %v", tc.name, m["id"], tc.id)
		}
	}
}

func TestNotificationsGetNoResponse(t *testing.T) {
	s := testServer(nil)
	for _, msg := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
		`{"jsonrpc":"2.0","method":"unknown/thing"}`,
	} {
		if out := s.Handle(context.Background(), []byte(msg)); out != nil {
			t.Errorf("%s: got response %s", msg, out)
		}
	}
}

func TestPingAndToolsList(t *testing.T) {
	s := testServer(nil)
	if m := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); len(m["result"].(map[string]any)) != 0 {
		t.Errorf("ping = %v", m)
	}
	m := rpc(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := m["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %d", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "search" {
		t.Errorf("name = %v", tool["name"])
	}
	if tool["annotations"].(map[string]any)["readOnlyHint"] != true {
		t.Errorf("not read-only: %v", tool["annotations"])
	}
	schema := tool["inputSchema"].(map[string]any)
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Errorf("schema = %v", schema)
	}
	if req := schema["required"].([]any); len(req) != 1 || req[0] != "query" {
		t.Errorf("required = %v", req)
	}
	props := schema["properties"].(map[string]any)
	if props["limit"].(map[string]any)["maximum"] != float64(50) || props["actor"].(map[string]any)["enum"] == nil {
		t.Errorf("properties = %v", props)
	}
}

func TestToolCallArgv(t *testing.T) {
	var got []string
	s := testServer(func(_ context.Context, argv []string) (string, bool) {
		got = argv
		return `[]`, false
	})
	m := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"query":"--help x","actor":"human","by_session":true}}}`)
	res := m["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("isError: %v", res)
	}
	want := "search --actor=human --by-session --limit=20 --json -- --help x"
	if strings.Join(got, " ") != want {
		t.Errorf("argv = %q, want %q", strings.Join(got, " "), want)
	}
	c := res["content"].([]any)
	if len(c) != 1 || c[0].(map[string]any)["text"] != "[]" || c[0].(map[string]any)["type"] != "text" {
		t.Errorf("content = %v", c)
	}
}

func TestListParam(t *testing.T) {
	var got []string
	s := testServer(func(_ context.Context, argv []string) (string, bool) { got = argv; return "[]", false })
	for args, want := range map[string]string{
		`{"query":"q","type":["event","session"]}`: "--type=event,session",
		`{"query":"q","type":"session, event"}`:    "--type=session,event",
	} {
		rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":`+args+`}}`)
		if !strings.Contains(strings.Join(got, " "), want) {
			t.Errorf("%s: argv %v lacks %s", args, got, want)
		}
	}
	m := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"query":"q","type":["memory"]}}}`)
	if m["result"].(map[string]any)["isError"] != true {
		t.Errorf("enum not checked: %v", m)
	}
	list := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)["result"].(map[string]any)["tools"].([]any)
	typ := list[0].(map[string]any)["inputSchema"].(map[string]any)["properties"].(map[string]any)["type"].(map[string]any)
	if typ["anyOf"] == nil || typ["type"] != nil {
		t.Errorf("schema of a list = %v", typ)
	}
}

func TestToolCallValidation(t *testing.T) {
	ran := false
	s := testServer(func(context.Context, []string) (string, bool) { ran = true; return "", false })
	for name, args := range map[string]string{
		"missing":  `{}`,
		"unknown":  `{"query":"x","sql":"drop"}`,
		"enum":     `{"query":"x","actor":"robot"}`,
		"max":      `{"query":"x","limit":51}`,
		"negative": `{"query":"x","limit":-1}`,
		"type":     `{"query":5}`,
		"bool":     `{"query":"x","by_session":"yes"}`,
		"notobj":   `[1]`,
	} {
		m := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":`+args+`}}`)
		res, ok := m["result"].(map[string]any)
		if !ok || res["isError"] != true {
			t.Errorf("%s: %v", name, m)
			continue
		}
		var e struct {
			Error struct{ Kind, Message string }
		}
		if err := json.Unmarshal([]byte(res["content"].([]any)[0].(map[string]any)["text"].(string)), &e); err != nil || e.Error.Kind != "usage" || e.Error.Message == "" {
			t.Errorf("%s: error object = %v", name, res["content"])
		}
	}
	if ran {
		t.Error("runner ran on invalid arguments")
	}
}

func TestToolCallFailureKeepsCLIError(t *testing.T) {
	s := testServer(func(context.Context, []string) (string, bool) {
		return `{"error":{"kind":"db","message":"no archive"}}` + "\n", true
	})
	m := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"show","arguments":{"session":"x"}}}`)
	res := m["result"].(map[string]any)
	if res["isError"] != true || res["content"].([]any)[0].(map[string]any)["text"] != `{"error":{"kind":"db","message":"no archive"}}` {
		t.Errorf("result = %v", res)
	}
}

func items(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf(`{"i":%d,"t":"xxxxxxxxxx"}`, i)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestTruncateArray(t *testing.T) {
	s := testServer(func(context.Context, []string) (string, bool) { return items(40), false })
	m := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"query":"x"}}}`)
	c := m["result"].(map[string]any)["content"].([]any)
	if len(c) != 2 {
		t.Fatalf("content = %d items", len(c))
	}
	first := c[0].(map[string]any)["text"].(string)
	var kept []map[string]any
	if err := json.Unmarshal([]byte(first), &kept); err != nil || len(kept) == 0 || len(kept) >= 40 {
		t.Fatalf("first item: %v, %d kept", err, len(kept))
	}
	if len(first) > 200 {
		t.Errorf("first item %d bytes, max 200", len(first))
	}
	var h Hint
	if err := json.Unmarshal([]byte(c[1].(map[string]any)["text"].(string)), &h); err != nil {
		t.Fatal(err)
	}
	if !h.Truncated || h.Next != nil || h.Hint == "" {
		t.Errorf("hint = %+v (search has no offset)", h)
	}
}

func TestTruncateShowEventsWithOffset(t *testing.T) {
	doc := `{"session":{"id":"claude:a"},"events":` + items(40) + `}`
	s := testServer(func(context.Context, []string) (string, bool) { return doc, false })
	m := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"show","arguments":{"session":"a","offset":100}}}`)
	c := m["result"].(map[string]any)["content"].([]any)
	if len(c) != 2 {
		t.Fatalf("content = %d items", len(c))
	}
	first := c[0].(map[string]any)["text"].(string)
	var got struct {
		Session map[string]any
		Events  []json.RawMessage
	}
	if err := json.Unmarshal([]byte(first), &got); err != nil || got.Session["id"] != "claude:a" || len(got.Events) == 0 {
		t.Fatalf("first = %s (%v)", first, err)
	}
	if !strings.HasPrefix(first, `{"session"`) {
		t.Errorf("key order lost: %s", first)
	}
	var h Hint
	_ = json.Unmarshal([]byte(c[1].(map[string]any)["text"].(string)), &h)
	if h.Next == nil || h.Next.Offset != 100+len(got.Events) {
		t.Errorf("hint = %+v, kept %d", h, len(got.Events))
	}
}

func TestSmallResultIsVerbatim(t *testing.T) {
	s := testServer(func(context.Context, []string) (string, bool) { return "[\n  {\"a\": 1}\n]\n", false })
	m := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"query":"x"}}}`)
	c := m["result"].(map[string]any)["content"].([]any)
	if len(c) != 1 || c[0].(map[string]any)["text"] != "[\n  {\"a\": 1}\n]" {
		t.Errorf("content = %v", c)
	}
}

func TestServe(t *testing.T) {
	s := testServer(nil)
	in := `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"ping"}` // no trailing newline
	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"id":1`) || !strings.Contains(lines[1], `"id":2`) {
		t.Errorf("output = %q", out.String())
	}
}
