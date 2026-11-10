package inputkind

import (
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

func TestOf(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		remote bool
		want   model.InputKind
	}{
		{"typed", "please fix the build", false, model.InputTyped},
		{"empty text is typed", "", false, model.InputTyped},
		{"bash input", "<bash-input>git status</bash-input>", false, model.InputBash},
		{"bash input with leading blank", "\n <bash-input>ls</bash-input>", false, model.InputBash},
		{"pasted block alone", "<pasted_content id=\"1\">\nline\n</pasted_content id=\"1\">", false, model.InputPasted},
		{"pasted block after own text", "look at this:\n<pasted_content id=\"a\">x</pasted_content id=\"a\">", false, model.InputPasted},
		{"pasted tag without close is typed", "write <pasted_content in a sentence", false, model.InputTyped},
		{"other element is not pasted", "<pasted_contents>x</pasted_contents>", false, model.InputTyped},
		{"remote typed", "hello from the phone", true, model.InputRemote},
		{"remote beats pasted", "<pasted_content id=\"1\">x</pasted_content id=\"1\">", true, model.InputRemote},
		{"bash beats remote", "<bash-input>ls</bash-input>", true, model.InputBash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Of(tt.text, tt.remote); got != tt.want {
				t.Errorf("Of(%q, %v) = %q, want %q", tt.text, tt.remote, got, tt.want)
			}
		})
	}
}

func TestRemoteSignal(t *testing.T) {
	tests := []struct {
		entrypoint, originKind string
		want                   bool
	}{
		{"cli", "human", false},
		{"cli", "", false},
		{"", "", false},
		{"remote", "", true},
		{"remote-control", "human", true},
		{"cli", "remote", true},
		{"cli", "bridge", true},
		{"cli", "peer", false},
		{"sdk-cli", "", false},
	}
	for _, tt := range tests {
		if got := RemoteSignal(tt.entrypoint, tt.originKind); got != tt.want {
			t.Errorf("RemoteSignal(%q, %q) = %v, want %v", tt.entrypoint, tt.originKind, got, tt.want)
		}
	}
}

func TestFromRaw(t *testing.T) {
	tests := []struct {
		name, raw string
		want      model.InputKind
		ok        bool
	}{
		{"typed", `{"type":"user","entrypoint":"cli","origin":{"kind":"human"}}`, model.InputTyped, true},
		{"typed no origin", `{"type":"user","entrypoint":"cli","origin":null}`, model.InputTyped, true},
		{"remote entrypoint", `{"type":"user","entrypoint":"remote"}`, model.InputRemote, true},
		{"remote origin", `{"type":"user","origin":{"kind":"remote"}}`, model.InputRemote, true},
		{"unparseable", `not json`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := FromRaw("plain text", tt.raw)
			if ok != tt.ok || got != tt.want {
				t.Errorf("FromRaw = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
	// Text alone decides bash and pasted, even without usable raw.
	if got, ok := FromRaw("<bash-input>ls</bash-input>", ""); !ok || got != model.InputBash {
		t.Errorf("bash without raw = %q, %v", got, ok)
	}
	if got, ok := FromRaw("<pasted_content id=\"1\">x</pasted_content id=\"1\">", ""); !ok || got != model.InputPasted {
		t.Errorf("pasted without raw = %q, %v", got, ok)
	}
	if _, ok := FromRaw("plain", ""); ok {
		t.Errorf("plain text without raw must be unknown")
	}
}
