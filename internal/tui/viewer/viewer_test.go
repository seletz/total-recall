package viewer

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/theme"
)

var env = Env{Theme: theme.New(true), Location: time.UTC, Terms: []string{"cask"}}

func plain(c Content) string {
	lines := append(append([]string{}, c.Head...), c.Body...)
	for i, l := range lines {
		lines[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	return strings.Join(lines, "\n")
}

func info(name string) store.SessionInfo {
	s := store.SessionInfo{Events: 3, HumanEvents: 1, AgentEvents: 2}
	s.ID, s.Name, s.Harness = "claude:abc", name, "claude"
	s.StartedAt = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	return s
}

func event(id string, kind model.Kind, actor model.Actor, tool, text string) model.Event {
	return model.Event{ID: id, Kind: kind, Actor: actor, ToolName: tool, Text: text, TS: time.Date(2026, 10, 1, 9, 1, 0, 0, time.UTC)}
}

func TestPrettyJSON(t *testing.T) {
	tests := []struct {
		name, in, want string
		ok             bool
	}{
		{"object keeps key order", `{"b":1,"a":[1,2]}`, "{\n  \"b\": 1,\n  \"a\": [\n    1,\n    2\n  ]\n}", true},
		{"array", `[1]`, "[\n  1\n]", true},
		{"padded", " {\"a\":1}\n", "{\n  \"a\": 1\n}", true},
		{"text", "git status", "git status", false},
		{"cut off value", `{"a":"abc`, `{"a":"abc`, false},
		{"scalar", `"x"`, `"x"`, false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := prettyJSON(tt.in)
			if got != tt.want || ok != tt.ok {
				t.Errorf("prettyJSON(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestPairTool(t *testing.T) {
	events := []model.Event{
		event("m1", model.KindMessage, model.ActorHuman, "", "go"),
		event("u1", model.KindToolUse, model.ActorAgent, "Bash", "ls"),
		event("r1", model.KindToolResult, model.ActorTool, "", "a b"),
		event("u2", model.KindToolUse, model.ActorAgent, "Read", "x"),
		event("u3", model.KindToolUse, model.ActorAgent, "Read", "y"),
		event("r3", model.KindToolResult, model.ActorTool, "", "z"),
	}
	tests := []struct {
		at          int
		use, result string
	}{
		{1, "u1", "r1"},
		{2, "u1", "r1"},
		{3, "u2", ""}, // the next call comes before any result
		{4, "u3", "r3"},
		{5, "u3", "r3"},
	}
	id := func(e *model.Event) string {
		if e == nil {
			return ""
		}
		return e.ID
	}
	for _, tt := range tests {
		use, result := PairTool(events, tt.at)
		if id(use) != tt.use || id(result) != tt.result {
			t.Errorf("PairTool(%d) = %s, %s; want %s, %s", tt.at, id(use), id(result), tt.use, tt.result)
		}
	}
}

func TestTool(t *testing.T) {
	use := event("u", model.KindToolUse, model.ActorAgent, "Bash", `{"command":"brew info --cask kompass"}`)
	res := event("r", model.KindToolResult, model.ActorTool, "", `{"casks":[{"token":"kompass"}]}`)
	for _, hitResult := range []bool{false, true} {
		c := Tool{Session: info("Cask"), Use: &use, Result: &res, HitResult: hitResult}.Render(env, 60)
		out := plain(c)
		for _, want := range []string{"Cask", "tool_use Bash", "tool (tool_result)", "  \"command\": \"brew info --cask kompass\"", "    {", `      "token": "kompass"`} {
			if !strings.Contains(out, want) {
				t.Errorf("hit result %v: output lacks %q:\n%s", hitResult, want, out)
			}
		}
		if got := strings.Count(out, MarkText); got != 1 {
			t.Errorf("%d marks, want 1:\n%s", got, out)
		}
		marked := c.Body[c.Anchor]
		if want := map[bool]string{false: "tool_use", true: "tool_result"}[hitResult]; !strings.Contains(ansi.Strip(marked), want) {
			t.Errorf("anchor line %q lacks %s", ansi.Strip(marked), want)
		}
	}
	// A missing counterpart is said, not hidden.
	out := plain(Tool{Session: info("x"), Use: &use, HitResult: false}.Render(env, 60))
	if !strings.Contains(out, "no result recorded") {
		t.Errorf("missing result not shown:\n%s", out)
	}
}

func TestMemoryMarkdown(t *testing.T) {
	src := "---\nname: demo\n---\n\n# Title\n\nSome **bold** text with `code` and a cask.\n\n- item one\n  - nested\n\n```\nraw **kept**\n```\n"
	c := Memory{Path: "demo.md", Project: "kompass", Harness: "claude", Changed: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), Content: src}.Render(env, 40)
	out := plain(c)
	for _, want := range []string{"demo.md", "kompass  claude", "2026-10-01 09:00", " name: demo", " Title", " Some bold text with code and a cask.", " • item one", "   • nested", "   raw **kept**"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{"# Title", "```", "**bold**"} {
		if strings.Contains(out, bad) {
			t.Errorf("output keeps %q:\n%s", bad, out)
		}
	}
	if c.Anchor != -1 {
		t.Errorf("anchor = %d: a match near the top leaves the file at its start", c.Anchor)
	}
	deep := Memory{Content: strings.Repeat("filler\n\n", topLines) + "the cask is here\n"}.Render(env, 40)
	if deep.Anchor < topLines || !strings.Contains(ansi.Strip(deep.Body[deep.Anchor]), "cask") {
		t.Errorf("anchor %d is not the line with the deep match", deep.Anchor)
	}
	if got := (Memory{Content: "plain"}).Render(Env{Theme: env.Theme}, 40); got.Anchor != -1 {
		t.Errorf("anchor without match = %d, want -1", got.Anchor)
	}
	// The lines of a paragraph are joined and wrapped to the width.
	para := plain(Memory{Content: "one two\nthree four\n\nnext"}.Render(env, 40))
	if !strings.Contains(para, " one two three four\n") {
		t.Errorf("paragraph not joined:\n%s", para)
	}
}

func TestSessionViewer(t *testing.T) {
	var first []model.Event
	long := strings.Repeat("word ", 200)
	first = append(first, event("1", model.KindMessage, model.ActorHuman, "", "release the cask"), event("2", model.KindMessage, model.ActorAgent, "", long))
	c := Session{Info: info("Cask"), First: first}.Render(env, 50)
	out := plain(c)
	for _, want := range []string{"Cask", "3 events (1 human, 2 agent)  claude", "release the cask", "…"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if n := len(c.Body); n > 2+2*(LinesPerMessage+2) {
		t.Errorf("body has %d lines, messages are not clipped", n)
	}
	if got := plain(Session{Info: info("x")}.Render(env, 50)); !strings.Contains(got, "no messages") {
		t.Errorf("empty session:\n%s", got)
	}
}

func TestExchange(t *testing.T) {
	events := []model.Event{
		event("1", model.KindMessage, model.ActorHuman, "", "what about the cask?"),
		event("2", model.KindMessage, model.ActorAgent, "", "it is published"),
	}
	c := Exchange{Session: info("Cask"), Events: events, Hit: 1}.Render(env, 50)
	out := plain(c)
	if !strings.Contains(out, "agent ◄") || strings.Count(out, MarkText) != 1 {
		t.Errorf("hit not marked once:\n%s", out)
	}
	if !strings.Contains(ansi.Strip(c.Body[c.Anchor]), "agent") {
		t.Errorf("anchor %d: %q", c.Anchor, c.Body[c.Anchor])
	}
}

// Every viewer renders within its width and survives a degenerate one.
func TestWidths(t *testing.T) {
	use := event("u", model.KindToolUse, model.ActorAgent, "Bash", strings.Repeat("x", 300))
	vs := []Viewer{
		Exchange{Session: info("x"), Events: []model.Event{use}, Hit: 0},
		Tool{Session: info("x"), Use: &use},
		Session{Info: info("x"), First: []model.Event{use}},
		Memory{Path: "p.md", Content: "# h\n" + strings.Repeat("y", 300)},
	}
	for _, v := range vs {
		for _, w := range []int{1, 5, 20, 80} {
			c := v.Render(env, w)
			for _, l := range append(c.Head, c.Body...) {
				if got := ansi.StringWidth(l); got > w+len(MarkText) && w > 12 {
					t.Errorf("%T at width %d: line of width %d: %q", v, w, got, ansi.Strip(l))
				}
			}
		}
	}
}
