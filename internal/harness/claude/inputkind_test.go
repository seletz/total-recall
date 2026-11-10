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
