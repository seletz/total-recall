package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// What of a transcript line survives in events.raw. The archive keeps raw as
// a reserve for re-reading an event without its transcript, so only the
// fields the parser (parseLine) and the reclassifier (Reclassify) look at
// stay; everything the store keeps in its own columns (session, cwd, git
// branch, harness version, token counts) and Claude Code's bookkeeping
// (wire*, serverClassifier*, request IDs, permission decisions, ...) goes.
// The lists are the single place that decides; the store is harness-neutral
// and only compresses what TrimRaw returns.
var (
	// rawKeep are the top-level fields kept, in output order.
	rawKeep = []string{
		"type", "uuid", "parentUuid", "timestamp",
		"isMeta", "isCompactSummary", "scheduledTaskId", "origin", "promptSource",
		"subtype", "content", // system records
		"message", "toolUseResult",
	}
	// rawMessageKeep are the fields of "message" kept; usage (token counts
	// are columns), id, role, stop_reason and the rest go.
	rawMessageKeep = []string{"model", "content"}
	// rawToolUseResultDrop are the fields of an object-valued
	// "toolUseResult" that go: the edit diffs repeat what message.content
	// already carries in another form and are the bulk of it.
	rawToolUseResultDrop = []string{"bashEditDiff", "structuredPatch"}
)

// RawKeptFields returns the top-level fields TrimRaw keeps (for the docs
// and the format check).
func RawKeptFields() []string { return append([]string(nil), rawKeep...) }

// TrimRaw implements harness.RawTrimmer: it reduces a transcript line to
// the fields listed in rawKeep, rawMessageKeep and rawToolUseResultDrop.
// Values are copied byte for byte; the output is compact JSON with the keys
// in a fixed order, so the result is deterministic and idempotent. A line
// that is not a JSON object is an error.
func (a *Adapter) TrimRaw(raw []byte) ([]byte, error) { return TrimRaw(raw) }

// TrimRaw is Adapter.TrimRaw as a function.
func TrimRaw(raw []byte) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("claude: trim raw: %w", err)
	}
	if top == nil {
		return nil, fmt.Errorf("claude: trim raw: not a JSON object")
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	n := 0
	for _, k := range rawKeep {
		v, ok := top[k]
		if !ok {
			continue
		}
		switch k {
		case "message":
			v = keepFields(v, rawMessageKeep)
		case "toolUseResult":
			v = dropFields(v, rawToolUseResultDrop)
		}
		if v == nil {
			continue
		}
		writeField(&buf, k, v, n)
		n++
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// keepFields returns the object v reduced to keys, in that order, or v
// itself if it is not an object; nil if nothing is left.
func keepFields(v json.RawMessage, keys []string) json.RawMessage {
	obj, ok := asObject(v)
	if !ok {
		return v
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	n := 0
	for _, k := range keys {
		if fv, ok := obj[k]; ok {
			writeField(&buf, k, fv, n)
			n++
		}
	}
	if n == 0 {
		return nil
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

// dropFields returns the object v without keys (remaining keys sorted), or
// v itself if it is not an object.
func dropFields(v json.RawMessage, keys []string) json.RawMessage {
	obj, ok := asObject(v)
	if !ok {
		return v
	}
	for _, k := range keys {
		delete(obj, k)
	}
	b, err := marshalObject(obj)
	if err != nil {
		return v
	}
	return b
}

func asObject(v json.RawMessage) (map[string]json.RawMessage, bool) {
	t := bytes.TrimLeft(v, " \t\r\n")
	if len(t) == 0 || t[0] != '{' {
		return nil, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(v, &obj); err != nil || obj == nil {
		return nil, false
	}
	return obj, true
}

// marshalObject encodes obj with sorted keys and without HTML escaping of
// the values (they are copied as they are).
func marshalObject(obj map[string]json.RawMessage) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(obj); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func writeField(buf *bytes.Buffer, key string, v json.RawMessage, n int) {
	if n > 0 {
		buf.WriteByte(',')
	}
	k, _ := json.Marshal(key)
	buf.Write(k)
	buf.WriteByte(':')
	buf.Write(v)
}
