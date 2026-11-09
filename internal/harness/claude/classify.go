package claude

import (
	"encoding/json"
	"strings"

	"github.com/seletz/total-recall/internal/model"
)

// Classifier decides the actor of a user-role text block. Prefix lists are
// matched against the text with leading whitespace removed.
type Classifier struct {
	// PeerPrefixes mark messages from other agent sessions.
	PeerPrefixes []string
	// PeerPasteHeaders mark a message that consists of exactly one pasted
	// block (<pasted_content …>) whose first non-blank line starts with one
	// of them: a task a coordinating agent pasted into the terminal. Checked
	// after PeerPrefixes and before HumanPrefixes.
	PeerPasteHeaders []string
	// HumanPrefixes mark harness-wrapped input the person typed or pasted
	// (checked after peer, before automation prefixes and record flags).
	HumanPrefixes []string
	// AutomationPrefixes mark harness-injected or scheduled input.
	AutomationPrefixes []string
}

// DefaultClassifier returns the built-in prefix lists (docs/explanation/design.md, Actor-Klassifikation).
func DefaultClassifier() Classifier {
	return Classifier{
		PeerPrefixes: []string{
			"Another Claude session",
			"<agent-message",
			"[Cross-session",
		},
		HumanPrefixes: []string{
			"<bash-input>",
			"<pasted_content",
		},
		AutomationPrefixes: []string{
			"<task-notification>",
			"<command-",
			"<local-command-",
			"<bash-stdout>",
			"<bash-stderr>",
			"<system-reminder>",
			"This session is being continued",
			"Base directory for this skill",
			"[Request interrupted",
		},
	}
}

// userFlags are the record-level signals relevant for classification.
type userFlags struct {
	originKind    string // origin.kind: human, peer, task-notification, ...
	isMeta        bool
	compact       bool
	scheduled     bool
	subagentInput bool // record lives in a subagent transcript: its prompts come from the parent agent
}

// Classify returns the actor for a user text block. Order: text rules
// (ClassifyText), then origin.kind, then isMeta/compact/scheduled flags,
// then subagent transcripts (peer), then human.
func (c Classifier) Classify(text string, f userFlags) model.Actor {
	if a, ok := c.ClassifyText(text); ok {
		return a
	}
	switch f.originKind {
	case "peer":
		return model.ActorPeer
	case "", "human", "remote", "remote-control", "bridge":
	default: // task-notification, scheduled, system, ...
		return model.ActorAutomation
	}
	if f.isMeta || f.compact || f.scheduled {
		return model.ActorAutomation
	}
	if f.subagentInput {
		return model.ActorPeer
	}
	return model.ActorHuman
}

// ClassifyText applies the rules that look at the text alone: peer
// prefixes, peer paste headers, human prefixes, automation prefixes, in
// that order. ok is false when none matches; the actor then depends on the
// record flags (Classify).
func (c Classifier) ClassifyText(text string) (actor model.Actor, ok bool) {
	t := strings.TrimLeft(text, " \t\r\n")
	switch {
	case hasAnyPrefix(t, c.PeerPrefixes):
		return model.ActorPeer, true
	case c.pastedPeer(t):
		return model.ActorPeer, true
	case hasAnyPrefix(t, c.HumanPrefixes):
		return model.ActorHuman, true
	case hasAnyPrefix(t, c.AutomationPrefixes):
		return model.ActorAutomation, true
	}
	return "", false
}

func (c Classifier) pastedPeer(t string) bool {
	if len(c.PeerPasteHeaders) == 0 {
		return false
	}
	h, ok := pastedHeader(t)
	return ok && hasAnyPrefix(h, c.PeerPasteHeaders)
}

const (
	pasteOpen  = "<pasted_content"
	pasteClose = "</pasted_content"
)

// pastedHeader returns the first non-blank line of a message that consists
// of exactly one pasted block (<pasted_content …>…</pasted_content …>) and
// nothing else but whitespace. ok is false for any other shape, including
// a block with own text before or after it, two blocks, or an empty block.
func pastedHeader(text string) (header string, ok bool) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, pasteOpen) {
		return "", false
	}
	start := strings.IndexByte(t, '>')
	if start < 0 {
		return "", false
	}
	// The opening tag is "<pasted_content" followed by whitespace or ">",
	// not another element name.
	if rest := t[len(pasteOpen):start]; rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return "", false
	}
	end := strings.LastIndex(t, pasteClose)
	if end < start {
		return "", false
	}
	closeEnd := strings.IndexByte(t[end:], '>')
	if closeEnd < 0 || strings.TrimSpace(t[end+closeEnd+1:]) != "" {
		return "", false
	}
	body := t[start+1 : end]
	if strings.Contains(body, pasteOpen) {
		return "", false
	}
	for _, line := range strings.Split(body, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			return l, true
		}
	}
	return "", false
}

// Reclassify implements harness.Reclassifier: it derives the actor of a
// stored user message again from its text and, when the text rules do not
// decide, from the record flags in its raw JSON. subagent says whether the
// event's session is a subagent transcript. ok is false when the text rules
// do not match and the raw record is missing or unreadable; the stored actor
// then stays.
func (a *Adapter) Reclassify(ev model.Event, subagent bool) (actor model.Actor, ok bool) {
	if act, ok := a.classifier.ClassifyText(ev.Text); ok {
		return act, true
	}
	if ev.Raw == "" {
		return "", false
	}
	var r rawRecord
	if err := json.Unmarshal([]byte(ev.Raw), &r); err != nil {
		return "", false
	}
	flags := userFlags{
		originKind:    originKind(r.Origin),
		isMeta:        r.IsMeta,
		compact:       r.IsCompactSummary,
		scheduled:     r.ScheduledTaskID != "",
		subagentInput: subagent,
	}
	return a.classifier.Classify(ev.Text, flags), true
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
