// Package skill embeds the agent skill (SKILL.md) printed by
// `total-recall llm --skill`.
package skill

import _ "embed"

//go:embed SKILL.md
var text string

// Text returns the embedded SKILL.md: instructions for coding agents on how
// to query the total-recall archive (commands, schema, example SQL, caveats).
func Text() string {
	return text
}
