package claude

import (
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

// paste wraps body in one pasted_content block the way the harness records it.
func paste(id, body string) string {
	return "<pasted_content id=\"" + id + "\">\n" + body + "\n</pasted_content id=\"" + id + "\">\n"
}

func TestPastedHeader(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
		ok   bool
	}{
		{"single block", paste("1c6c", "# Task: fix the build\nDetails follow."), "# Task: fix the build", true},
		{"blank lines before header", paste("1", "\n\n  Auftrag: PR anlegen\nmehr"), "Auftrag: PR anlegen", true},
		{"crlf", "<pasted_content id=\"2\">\r\n# Task: x\r\n</pasted_content id=\"2\">\r\n", "# Task: x", true},
		{"leading whitespace in message", "\n  " + paste("3", "# Task: y"), "# Task: y", true},
		{"no trailing newline", "<pasted_content id=\"4\">\n# Task: z</pasted_content id=\"4\">", "# Task: z", true},
		{"empty block", paste("5", ""), "", false},
		{"text after block", paste("6", "# Task: a") + "please do it", "", false},
		{"text before block", "look: " + paste("7", "# Task: a"), "", false},
		{"two blocks", paste("8", "# Task: a") + paste("9", "# Task: b"), "", false},
		{"unterminated", "<pasted_content id=\"10\">\n# Task: a\n", "", false},
		{"plain text", "# Task: typed by hand", "", false},
		{"other tag", "<pasted_contents id=\"11\">\n# Task: a\n</pasted_contents id=\"11\">", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := pastedHeader(tt.text)
			if got != tt.want || ok != tt.ok {
				t.Errorf("pastedHeader = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestClassifyPeerPasteHeaders(t *testing.T) {
	c := DefaultClassifier()
	c.PeerPrefixes = append(c.PeerPrefixes, "From coordinator:")
	c.PeerPasteHeaders = []string{"# Task:", "Auftrag:"}
	human := userFlags{originKind: "human"}
	tests := []struct {
		name string
		text string
		want model.Actor
	}{
		{"task header", paste("1", "# Task: fix the build\nDetails."), model.ActorPeer},
		{"german header", paste("2", "Auftrag: Issue #7 umsetzen"), model.ActorPeer},
		{"header after blank line", paste("3", "\n# Task: later"), model.ActorPeer},
		{"other header stays human", paste("4", "# Notes from the meeting"), model.ActorHuman},
		{"log paste stays human", paste("5", "ERROR task failed\n# Task: not first"), model.ActorHuman},
		{"header plus own text stays human", paste("6", "# Task: a") + "what do you think?", model.ActorHuman},
		{"typed header stays human", "# Task: I typed this", model.ActorHuman},
		{"peer prefix still wins", "From coordinator: rebase", model.ActorPeer},
		{"automation untouched", "<system-reminder>x</system-reminder>", model.ActorAutomation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Classify(tt.text, human); got != tt.want {
				t.Errorf("Classify = %s, want %s", got, tt.want)
			}
		})
	}
	// Without configured headers a task paste is what the person pasted.
	if got := DefaultClassifier().Classify(paste("9", "# Task: a"), human); got != model.ActorHuman {
		t.Errorf("default classifier: paste = %s, want human", got)
	}
}

func TestClassifyText(t *testing.T) {
	c := DefaultClassifier()
	c.PeerPasteHeaders = []string{"# Task:"}
	tests := []struct {
		name string
		text string
		want model.Actor
		ok   bool
	}{
		{"peer prefix", "Another Claude session sent a message: hi", model.ActorPeer, true},
		{"paste header", paste("1", "# Task: a"), model.ActorPeer, true},
		{"human prefix", "<bash-input>ls</bash-input>", model.ActorHuman, true},
		{"automation prefix", "<task-notification>x</task-notification>", model.ActorAutomation, true},
		{"plain text undecided", "hello", "", false},
		{"other paste undecided", paste("2", "log lines"), model.ActorHuman, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := c.ClassifyText(tt.text)
			if got != tt.want || ok != tt.ok {
				t.Errorf("ClassifyText = %s, %v; want %s, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

// TestReclassify re-derives the actor of stored user messages from text and
// raw record, as ingest --reclassify does after the rules changed.
func TestReclassify(t *testing.T) {
	a := New("", WithPeerPasteHeaders("# Task:"), WithPeerPrefixes("From coordinator:"))
	tests := []struct {
		name     string
		text     string
		raw      string
		subagent bool
		want     model.Actor
		ok       bool
	}{
		{"paste header from text alone", paste("1", "# Task: a"), "", false, model.ActorPeer, true},
		{"peer prefix from text alone", "From coordinator: go", "", false, model.ActorPeer, true},
		{"plain text without raw undecided", "hello", "", false, "", false},
		{"plain text with human origin", "hello", `{"type":"user","origin":{"kind":"human"}}`, false, model.ActorHuman, true},
		{"peer origin", "hi", `{"type":"user","isMeta":true,"origin":{"kind":"peer"}}`, false, model.ActorPeer, true},
		{"task notification origin", "done", `{"type":"user","origin":{"kind":"task-notification"}}`, false, model.ActorAutomation, true},
		{"isMeta", "look again", `{"type":"user","isMeta":true}`, false, model.ActorAutomation, true},
		{"scheduled", "Hourly check", `{"type":"user","scheduledTaskId":"s1"}`, false, model.ActorAutomation, true},
		{"compact summary", "summary", `{"type":"user","isCompactSummary":true}`, false, model.ActorAutomation, true},
		{"subagent prompt", "Research task", `{"type":"user"}`, true, model.ActorPeer, true},
		{"malformed raw undecided", "hello", `{not json`, false, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := a.Reclassify(model.Event{Text: tt.text, Raw: tt.raw}, tt.subagent)
			if got != tt.want || ok != tt.ok {
				t.Errorf("Reclassify = %s, %v; want %s, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
