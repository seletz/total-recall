package claude

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/model"
)

var _ harness.RawTrimmer = (*Adapter)(nil)

func TestTrimRaw(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string // "" = error
	}{
		{
			name: "user line keeps the record and classifier fields only",
			in: `{"parentUuid":"p-1","isSidechain":false,"userType":"external","cwd":"/w/x","sessionId":"s-1","version":"2.1.300","gitBranch":"main",` +
				`"type":"user","message":{"role":"user","content":"hello <b>&</b>"},"uuid":"u-1","timestamp":"2026-10-01T10:00:00.000Z",` +
				`"origin":{"kind":"human"},"promptSource":"typed","isMeta":false,"wireIngestContext":{"a":1},"serverClassifierContext":{"b":"c"},"requestId":"req_1","slug":"x","permissionDecision":"allow"}`,
			want: `{"type":"user","uuid":"u-1","parentUuid":"p-1","timestamp":"2026-10-01T10:00:00.000Z","isMeta":false,"origin":{"kind":"human"},"promptSource":"typed","message":{"content":"hello <b>&</b>"}}`,
		},
		{
			name: "assistant line drops usage, id, role and stop_reason, keeps content verbatim",
			in: `{"type":"assistant","uuid":"a-1","parentUuid":"u-1","timestamp":"2026-10-01T10:00:05.000Z","sessionId":"s-1","cwd":"/w/x","requestId":"req_2",` +
				`"message":{"id":"msg_1","type":"message","role":"assistant","model":"m-1","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls -la","description":"list"}}],` +
				`"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":5}},"wireToolInputs":[{"x":1}]}`,
			want: `{"type":"assistant","uuid":"a-1","parentUuid":"u-1","timestamp":"2026-10-01T10:00:05.000Z","message":{"model":"m-1","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls -la","description":"list"}}]}}`,
		},
		{
			name: "tool result drops the diffs of toolUseResult, keeps the rest sorted",
			in: `{"type":"user","uuid":"r-1","parentUuid":"a-1","timestamp":"2026-10-01T10:00:06.000Z","sessionId":"s-1","cwd":"/w/x",` +
				`"message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"ok","is_error":false}]},` +
				`"toolUseResult":{"stdout":"ok","stderr":"","interrupted":false,"structuredPatch":[{"lines":["-a","+b"]}],"bashEditDiff":"--- a\n+++ b","isImage":false},"sourceToolAssistantUUID":"a-1"}`,
			want: `{"type":"user","uuid":"r-1","parentUuid":"a-1","timestamp":"2026-10-01T10:00:06.000Z","message":{"content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"ok","is_error":false}]},` +
				`"toolUseResult":{"interrupted":false,"isImage":false,"stderr":"","stdout":"ok"}}`,
		},
		{
			name: "toolUseResult that is a string or array stays as it is",
			in:   `{"type":"user","uuid":"r-2","timestamp":"t","message":{"content":"x"},"toolUseResult":"Error: no such file"}`,
			want: `{"type":"user","uuid":"r-2","timestamp":"t","message":{"content":"x"},"toolUseResult":"Error: no such file"}`,
		},
		{
			name: "system line keeps subtype and content, drops level and cwd",
			in:   `{"type":"system","subtype":"turn_duration","content":"took 3s","level":"info","durationMs":3000,"uuid":"y-1","timestamp":"t","cwd":"/w","sessionId":"s","isMeta":true}`,
			want: `{"type":"system","uuid":"y-1","timestamp":"t","isMeta":true,"subtype":"turn_duration","content":"took 3s"}`,
		},
		{
			name: "message without kept fields is dropped entirely",
			in:   `{"type":"user","uuid":"u","timestamp":"t","message":{"role":"user"}}`,
			want: `{"type":"user","uuid":"u","timestamp":"t"}`,
		},
		{
			name: "scheduled prompt keeps its task id and compact flag",
			in:   `{"type":"user","uuid":"u","timestamp":"t","scheduledTaskId":"task-1","isCompactSummary":true,"message":{"content":"Hourly check"},"origin":"scheduled"}`,
			want: `{"type":"user","uuid":"u","timestamp":"t","isCompactSummary":true,"scheduledTaskId":"task-1","origin":"scheduled","message":{"content":"Hourly check"}}`,
		},
		{name: "array is an error", in: `[1,2]`},
		{name: "garbage is an error", in: `{"type":`},
		{name: "null is an error", in: `null`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TrimRaw([]byte(tt.in))
			if tt.want == "" {
				if err == nil {
					t.Fatalf("TrimRaw = %s, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("TrimRaw: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("TrimRaw =\n%s\nwant\n%s", got, tt.want)
			}
			if !json.Valid(got) {
				t.Errorf("output is not valid JSON: %s", got)
			}
			again, err := TrimRaw(got)
			if err != nil || string(again) != string(got) {
				t.Errorf("not idempotent: %s (%v)", again, err)
			}
		})
	}
}

// TestTrimRawKeepsReclassifyFlags: the reclassifier reads the record flags
// from the trimmed line and must decide as it does on the full line.
func TestTrimRawKeepsReclassifyFlags(t *testing.T) {
	a := New("")
	full := `{"type":"user","uuid":"u","timestamp":"t","sessionId":"s","cwd":"/w","origin":{"kind":"peer"},"promptSource":"typed","message":{"role":"user","content":"do it"},"serverClassifierContext":{}}`
	trimmed, err := TrimRaw([]byte(full))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{full, string(trimmed)} {
		actor, ok := a.Reclassify(model.Event{Text: "do it", Raw: raw}, false)
		if !ok || actor != model.ActorPeer {
			t.Errorf("Reclassify(%s) = %q, %v; want peer", raw, actor, ok)
		}
	}
}

// TestParseTrimsRaw: the parser emits the trimmed line as Event.Raw.
func TestParseTrimsRaw(t *testing.T) {
	line := `{"type":"user","uuid":"u-9","timestamp":"2026-10-01T10:00:00.000Z","sessionId":"` + sid + `","cwd":"/w","gitBranch":"main","version":"2.1.300","message":{"role":"user","content":"hi"},"wireIngestContext":{"k":1}}`
	p := writeFile(t, "p/"+sid+".jsonl", line+"\n")
	recs, _ := parseFile(t, New(""), p, 0)
	var raws []string
	for _, r := range recs {
		for _, e := range r.Events {
			raws = append(raws, e.Raw)
		}
	}
	want := []string{`{"type":"user","uuid":"u-9","timestamp":"2026-10-01T10:00:00.000Z","message":{"content":"hi"}}`}
	if !reflect.DeepEqual(raws, want) {
		t.Errorf("raws = %q, want %q", raws, want)
	}
	if strings.Contains(raws[0], "wireIngestContext") {
		t.Error("bookkeeping survived")
	}
}
