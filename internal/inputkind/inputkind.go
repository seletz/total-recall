// Package inputkind tells how a human input reached the harness: typed,
// pasted, as !cmd bash input or through Remote Control. The rules are
// shared by the adapter (ingest) and the schema migration that fills
// existing events, so both give the same answer. Pasted covers a
// <pasted_content> block and a pasted image or document (its marker or
// placeholder in the text); dictation into the input field is typed, the
// harness does not tell it from typing.
package inputkind

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/seletz/total-recall/internal/model"
)

const (
	bashOpen   = "<bash-input>"
	pasteOpen  = "<pasted_content"
	pasteClose = "</pasted_content"
)

// Of returns the input kind of a human message text. remote says that the
// record carries a Remote Control signal (RemoteSignal). Order: bash, remote,
// pasted, typed.
func Of(text string, remote bool) model.InputKind {
	switch {
	case strings.HasPrefix(strings.TrimLeft(text, " \t\r\n"), bashOpen):
		return model.InputBash
	case remote:
		return model.InputRemote
	case hasPaste(text), hasAttachment(text):
		return model.InputPasted
	}
	return model.InputTyped
}

// attachmentMark matches the marker of a pasted image or document: the
// harness's own "[Image #2]" in the text of the message, and the
// placeholder the adapter writes for an image or document block
// ("[image image/png 84.5 KiB]", "[document application/pdf]"). Matched
// anywhere in the text, so own text next to the attachment still counts.
var attachmentMark = regexp.MustCompile(`(?i)\[(?:image #[0-9]+|(?:image|document) [a-z]+/[^\]\n]*)\]`)

// bareAttachment is the placeholder of a block without media type ("[image]",
// "[document]"); only the whole text counts, a bracketed word in a sentence
// does not.
var bareAttachment = regexp.MustCompile(`(?i)^\s*\[(?:image|document)\]\s*$`)

// hasAttachment reports whether text carries the marker of a pasted image or
// document.
func hasAttachment(text string) bool {
	return attachmentMark.MatchString(text) || bareAttachment.MatchString(text)
}

// hasPaste reports whether text contains a complete pasted block: an
// opening <pasted_content …> tag followed later by a closing tag.
func hasPaste(text string) bool {
	for from := 0; ; {
		i := strings.Index(text[from:], pasteOpen)
		if i < 0 {
			return false
		}
		i += from + len(pasteOpen)
		if i < len(text) && (text[i] == ' ' || text[i] == '\t' || text[i] == '>') &&
			strings.Contains(text[i:], pasteClose) {
			return true
		}
		from = i
	}
}

// RemoteSignal reports whether the entrypoint or origin kind of a Claude
// Code record marks input sent through Remote Control. These values have
// not been observed in a local archive; they are the names the harness uses
// for its remote entry (see the PR description of #236).
func RemoteSignal(entrypoint, originKind string) bool {
	for _, v := range []string{entrypoint, originKind} {
		switch v {
		case "remote", "remote-control", "bridge":
			return true
		}
	}
	return false
}

// FromRaw derives the input kind of a stored human message from its text
// and its raw Claude Code line. Text alone decides bash and pasted; typed
// needs a readable raw line to rule out Remote Control. ok is false when
// the kind stays unknown.
func FromRaw(text, raw string) (model.InputKind, bool) {
	if k := Of(text, false); k == model.InputBash || k == model.InputPasted {
		// A remote paste is still remote: only skip the raw read when
		// there is none.
		if raw == "" {
			return k, true
		}
	}
	if raw == "" {
		return "", false
	}
	var r struct {
		Entrypoint string          `json:"entrypoint"`
		Origin     json.RawMessage `json:"origin"`
	}
	if json.Unmarshal([]byte(raw), &r) != nil {
		if k := Of(text, false); k == model.InputBash || k == model.InputPasted {
			return k, true
		}
		return "", false
	}
	return Of(text, RemoteSignal(r.Entrypoint, OriginKind(r.Origin))), true
}

// OriginKind returns the "kind" of a record's origin object, "" when it is
// missing or not an object.
func OriginKind(origin json.RawMessage) string {
	var o struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(origin, &o) != nil {
		return ""
	}
	return o.Kind
}
