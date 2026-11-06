package mcp

import (
	"bytes"
	"encoding/json"
)

// Hint is the second content item of a truncated result.
type Hint struct {
	Truncated bool   `json:"truncated"`
	Next      *Next  `json:"next,omitempty"`
	Hint      string `json:"hint"`
}

// Next says how to continue a truncated result.
type Next struct {
	Offset int `json:"offset"`
}

// truncate shortens out to at most max bytes when it is a JSON array or an
// object with an "events" array, keeping whole items so the result stays
// valid CLI JSON. offset is the paging offset of the call, used for
// next.offset (only when paged: the tool has an offset argument). A result that cannot be shortened is returned unchanged.
func truncate(out string, max, offset int, paged bool) (string, *Hint) {
	if len(out) <= max {
		return out, nil
	}
	b := []byte(out)
	if len(b) > 0 && b[0] == '[' {
		var items []json.RawMessage
		if json.Unmarshal(b, &items) != nil {
			return out, nil
		}
		kept := fit(items, max-2)
		short, _ := json.Marshal(items[:kept])
		return string(short), hintFor(kept, len(items), offset, paged)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil {
		return out, nil
	}
	raw, ok := obj["events"]
	if !ok {
		return out, nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return out, nil
	}
	rest := 0
	for k, v := range obj {
		if k != "events" {
			rest += len(k) + len(v) + 4
		}
	}
	kept := fit(items, max-rest-12)
	obj["events"], _ = json.Marshal(items[:kept])
	// Keep the key order of the CLI: session first, then events.
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	for _, k := range orderedKeys(b) {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(obj[k])
	}
	buf.WriteByte('}')
	return buf.String(), hintFor(kept, len(items), offset, paged)
}

// fit returns how many leading items fit into budget bytes (at least one,
// so a continuation always makes progress).
func fit(items []json.RawMessage, budget int) int {
	n, size := 0, 0
	for _, it := range items {
		size += len(it) + 1
		if size > budget && n > 0 {
			break
		}
		n++
	}
	return n
}

func hintFor(kept, total, offset int, paged bool) *Hint {
	h := &Hint{Truncated: true}
	if paged {
		h.Next = &Next{Offset: offset + kept}
		h.Hint = "output cut after " + itoa(kept) + " of " + itoa(total) + " items; call again with offset=" + itoa(offset+kept) + " to continue, or narrow the window (since, until, limit)"
	} else {
		h.Hint = "output cut after " + itoa(kept) + " of " + itoa(total) + " items; narrow the query (since, until, project, a smaller limit) to see the rest"
	}
	return h
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// orderedKeys returns the top-level keys of a JSON object in document order.
func orderedKeys(b []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(b))
	var keys []string
	if _, err := dec.Token(); err != nil {
		return nil
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		keys = append(keys, t.(string))
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}
