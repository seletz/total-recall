package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Hint is the second content item of a truncated result.
type Hint struct {
	Truncated bool  `json:"truncated"`
	Next      *Next `json:"next,omitempty"`
	// Cut names the nested lists that were shortened.
	Cut []Cut `json:"cut,omitempty"`
	// Shortened counts long text values that were cut.
	Shortened int    `json:"shortened_strings,omitempty"`
	Hint      string `json:"hint"`

	// kept and total count the items of the list cut in the first stage.
	kept, total int
}

// Cut says how much of the nested lists at one path survived. Path has []
// for every list level, e.g. worktime.days[].categories[].sessions; Lists is
// the number of such lists, Kept and Total count their entries together.
type Cut struct {
	Path  string `json:"path"`
	Lists int    `json:"lists"`
	Kept  int    `json:"kept"`
	Total int    `json:"total"`
}

// Next says how to continue a truncated result.
type Next struct {
	Offset int `json:"offset"`
}

// truncate bounds out to max bytes and keeps it valid JSON.
//
// A pretty-printed result is compacted first. A JSON array, or an object with
// an "events" array, is then cut after whole items (offset is the paging
// offset of the call, used for next.offset, only when paged: the tool has an
// offset argument). What still does not fit is cut structurally: the
// deepest nested lists first, the largest among them, one entry at a time
// until it fits, then the longest text values. narrow says how to ask for
// less. A result that is not JSON is returned unchanged.
func truncate(out string, max, offset int, paged bool, narrow string) (string, *Hint) {
	if len(out) <= max {
		return out, nil
	}
	var cb bytes.Buffer
	if json.Compact(&cb, []byte(out)) != nil {
		return out, nil
	}
	if cb.Len() <= max {
		return cb.String(), nil
	}
	cur, hint := cutList(cb.Bytes(), max, offset, paged)
	if len(cur) > max {
		var nested *Hint
		cur, nested = cutNested(cur, max, hint != nil && hint.Next != nil)
		hint = mergeHint(hint, nested)
	}
	if hint == nil {
		return string(cur), nil
	}
	hint.Truncated = true
	hint.Hint = hintText(hint, paged, narrow)
	return string(cur), hint
}

// cutList is the first stage: whole items of a top-level array or of the
// events of an object. It returns b unchanged and a nil hint for anything
// else.
func cutList(b []byte, max, offset int, paged bool) ([]byte, *Hint) {
	if b[0] == '[' {
		var items []json.RawMessage
		if json.Unmarshal(b, &items) != nil {
			return b, nil
		}
		kept := fit(items, max-2)
		short, _ := json.Marshal(items[:kept])
		return short, listHint(kept, len(items), offset, paged)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil {
		return b, nil
	}
	raw, ok := obj["events"]
	if !ok {
		return b, nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return b, nil
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
	return buf.Bytes(), listHint(kept, len(items), offset, paged)
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

func listHint(kept, total, offset int, paged bool) *Hint {
	h := &Hint{Truncated: true, kept: kept, total: total}
	if paged {
		h.Next = &Next{Offset: offset + kept}
	}
	return h
}

// hintText words the second content item: what was cut, how to continue,
// how to narrow.
func hintText(h *Hint, paged bool, narrow string) string {
	var parts []string
	if h.total > 0 {
		parts = append(parts, "list cut after "+itoa(h.kept)+" of "+itoa(h.total)+" items")
	}
	for _, c := range h.Cut {
		parts = append(parts, c.Path+": kept "+itoa(c.Kept)+" of "+itoa(c.Total)+" entries in "+itoa(c.Lists)+" lists")
	}
	if h.Shortened > 0 {
		parts = append(parts, itoa(h.Shortened)+" long text values shortened")
	}
	msg := "output cut: " + strings.Join(parts, "; ")
	if h.Next != nil {
		msg += "; call again with offset=" + itoa(h.Next.Offset) + " to continue, or narrow the window (since, until, limit)"
	} else if narrow != "" {
		msg += "; narrow the query to see the rest: " + narrow
	} else {
		msg += "; narrow the query (since, until, project, a smaller limit) to see the rest"
	}
	return msg
}

// mergeHint combines the list stage and the nested stage.
func mergeHint(a, b *Hint) *Hint {
	if a == nil {
		return b
	}
	if b != nil {
		a.Cut, a.Shortened = b.Cut, b.Shortened
	}
	return a
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
