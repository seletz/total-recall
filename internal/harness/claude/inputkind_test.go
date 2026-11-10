package claude

import (
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

func TestInputKind(t *testing.T) {
	tests := []struct {
		name    string
		extra   string
		content string
		want    model.InputKind
	}{
		{"typed", `"entrypoint":"cli","origin":{"kind":"human"},"promptSource":"typed"`, `"fix the test"`, model.InputTyped},
		{"typed without origin", `"entrypoint":"cli"`, `"hello"`, model.InputTyped},
		{"queued is still typed", `"entrypoint":"cli","promptSource":"queued"`, `"next thing"`, model.InputTyped},
		{"pasted block", `"entrypoint":"cli"`, `"<pasted_content id=\"1\">blob</pasted_content id=\"1\"> see above"`, model.InputPasted},
		{"pasted in text blocks", ``, `[{"type":"text","text":"review:\n<pasted_content id=\"1\">x</pasted_content id=\"1\">"}]`, model.InputPasted},
		{"pasted image", ``, `[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]`, model.InputPasted},
		{"bash", `"entrypoint":"cli"`, `"<bash-input>git pull</bash-input>"`, model.InputBash},
		{"remote entrypoint", `"entrypoint":"remote"`, `"from the phone"`, model.InputRemote},
		{"remote origin", `"origin":{"kind":"remote"}`, `"from the phone"`, model.InputRemote},
		{"peer has no kind", `"origin":{"kind":"peer"}`, `"Another Claude session sent a message: hi"`, ""},
		{"automation has no kind", `"origin":{"kind":"task-notification"}`, `"done"`, ""},
		{"bash stdout has no kind", ``, `"<bash-stdout>x</bash-stdout>"`, ""},
		{"tool result has no kind", ``, `[{"type":"tool_result","tool_use_id":"t1","content":"out"}]`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evs := parseLine(t, New(""), userLine(tt.extra, tt.content))
			if len(evs) != 1 {
				t.Fatalf("events = %d, want 1", len(evs))
			}
			if got := evs[0].InputKind; got != tt.want {
				t.Errorf("InputKind = %q, want %q (actor %s)", got, tt.want, evs[0].Actor)
			}
		})
	}
}

// TestInputKindImageMessage: a message with an image block and the
// harness's own marker in a text block gives only pasted events, with or
// without own text, and a stored placeholder event is pasted again when the
// kind is derived from the stored text and raw line.
func TestInputKindImageMessage(t *testing.T) {
	img := `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}`
	tests := []struct {
		name    string
		content string
		events  int
	}{
		{"marker and image", `[{"type":"text","text":"[Image #2]"},` + img + `]`, 2},
		{"own text, marker and image", `[{"type":"text","text":"what is this? [Image #3]"},` + img + `]`, 2},
		{"own text and image", `[{"type":"text","text":"what is this?"},` + img + `]`, 2},
		{"document", `[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"AAAA"}}]`, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := New("")
			line := userLine(`"entrypoint":"cli"`, tt.content)
			evs := parseLine(t, a, line)
			if len(evs) != tt.events {
				t.Fatalf("events = %d, want %d", len(evs), tt.events)
			}
			for _, e := range evs {
				if e.Actor != model.ActorHuman {
					t.Fatalf("actor = %s", e.Actor)
				}
				if tt.name == "own text and image" && e.Text == "what is this?" {
					// typed text next to an image: the message is
					// pasted as a whole only for the image event.
					continue
				}
				if e.InputKind != model.InputPasted {
					t.Errorf("%q: InputKind = %q, want pasted", e.Text, e.InputKind)
				}
				if k, ok := a.InputKind(model.Event{Text: e.Text, Raw: line}); !ok || (e.Text != "what is this?" && k != model.InputPasted) {
					t.Errorf("%q: stored kind = %q, %v", e.Text, k, ok)
				}
			}
		})
	}
}

func TestToolInput(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","content":[` +
		`{"type":"text","text":"ok"},` +
		`{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"herdr agent start w --cwd /w/a"}},` +
		`{"type":"tool_use","id":"t2","name":"Read","input":{"file_path":"/w/b.go"}}]}}`
	tests := []struct {
		name string
		id   string
		raw  string
		want string
		ok   bool
	}{
		{"first tool block", "claude:u#1", line, "herdr agent start w --cwd /w/a", true},
		{"second tool block, other key", "claude:u#2", line, "/w/b.go", true},
		{"text block", "claude:u#0", line, "", false},
		{"index out of range", "claude:u#9", line, "", false},
		{"no block in the ID", "claude:u", line, "", false},
		{"no raw line", "claude:u#1", "", "", false},
		{"unreadable raw", "claude:u#1", "not json", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := New("").ToolInput(model.Event{ID: tt.id, Raw: tt.raw})
			if got != tt.want || ok != tt.ok {
				t.Errorf("ToolInput = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
