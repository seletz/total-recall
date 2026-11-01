package claude

import (
	"strings"

	"github.com/seletz/total-recall/internal/model"
)

// Classifier decides the actor of a user-role text block. Prefix lists are
// matched against the text with leading whitespace removed.
type Classifier struct {
	// PeerPrefixes mark messages from other agent sessions.
	PeerPrefixes []string
	// HumanPrefixes mark harness-wrapped input the person typed or pasted
	// (checked after peer, before automation prefixes and record flags).
	HumanPrefixes []string
	// AutomationPrefixes mark harness-injected or scheduled input.
	AutomationPrefixes []string
}

// DefaultClassifier returns the built-in prefix lists (docs/design.md, Actor-Klassifikation).
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

// Classify returns the actor for a user text block. Order: explicit prefixes
// (peer, human, automation), then origin.kind, then isMeta/compact/scheduled
// flags, then subagent transcripts (peer), then human.
func (c Classifier) Classify(text string, f userFlags) model.Actor {
	t := strings.TrimLeft(text, " \t\r\n")
	switch {
	case hasAnyPrefix(t, c.PeerPrefixes):
		return model.ActorPeer
	case hasAnyPrefix(t, c.HumanPrefixes):
		return model.ActorHuman
	case hasAnyPrefix(t, c.AutomationPrefixes):
		return model.ActorAutomation
	}
	switch f.originKind {
	case "peer":
		return model.ActorPeer
	case "", "human":
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

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
