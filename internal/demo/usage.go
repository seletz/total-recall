package demo

import (
	"time"

	"github.com/seletz/total-recall/internal/testfixture"
)

// The invented models of the demo pool: the main sessions run on
// ModelLarge until ModelSwitch and on ModelNext from then on, the
// subagents always on ModelSmall. The demo archive itself keeps the model
// of the fixtures and has no tokens.
const (
	ModelLarge = "demo-large-4"
	ModelNext  = "demo-large-5"
	ModelSmall = "demo-small-4"
)

// ModelSwitch is when the main sessions of the pool move from ModelLarge
// to ModelNext: the Monday after the vacation in August.
var ModelSwitch = time.Date(2026, 8, 17, 0, 0, 0, 0, Location)

// Tokens of the system prompt and tools that open every conversation, so
// the first API call of a session writes them to the cache.
const (
	systemTokens    = 12_000
	subagentTokens  = 4_000
	freshTokens     = 3 // the uncached tail of every call
	charsPerToken   = 3
	outputBaseToken = 150 // thinking and the framing of every answer
)

// usage plays the prompt cache of one conversation: every API call reads
// what earlier calls cached, writes what was added since (prompt, tool
// results, its own previous output) and adds its own output. The numbers
// follow the length of the invented text, so they are deterministic and
// plausible in their proportions, not real.
type usage struct {
	subagent bool
	cached   int64 // tokens in the cache
	pending  int64 // tokens added since the last call
}

func newUsage(subagent bool) *usage {
	base := int64(systemTokens)
	if subagent {
		base = subagentTokens
	}
	return &usage{subagent: subagent, pending: base}
}

// add records text that enters the conversation (a prompt, a tool result).
func (u *usage) add(text string) { u.pending += tokens(text) }

// apply sets model and usage of an agent record at time at whose output
// is text.
func (u *usage) apply(l testfixture.Line, at time.Time, text string) {
	out := outputBaseToken + tokens(text)
	msg := l["message"].(map[string]any)
	msg["model"] = model(u.subagent, at)
	msg["usage"] = map[string]any{
		"input_tokens":                freshTokens,
		"cache_creation_input_tokens": u.pending,
		"cache_read_input_tokens":     u.cached,
		"output_tokens":               out,
	}
	u.cached += u.pending
	u.pending = out
}

// model is the model of an agent record at time at.
func model(subagent bool, at time.Time) string {
	switch {
	case subagent:
		return ModelSmall
	case at.Before(ModelSwitch):
		return ModelLarge
	}
	return ModelNext
}

func tokens(text string) int64 { return int64(len(text)/charsPerToken) + 1 }
