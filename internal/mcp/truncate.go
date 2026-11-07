package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Hint is the second content item of a result that is paged or cut.
type Hint struct {
	// Truncated says that parts of the result are missing and cannot be
	// fetched with a cursor: a page that alone is larger than the bound
	// was cut (see Cut and Shortened). Pages do not set it.
	Truncated bool `json:"truncated,omitempty"`
	// Page says which page of how many this is; Next how to get the one after.
	Page *PageInfo `json:"page,omitempty"`
	Next *Next     `json:"next,omitempty"`
	// Cut names the nested lists that were shortened.
	Cut []Cut `json:"cut,omitempty"`
	// Shortened counts long text values that were cut.
	Shortened int    `json:"shortened_strings,omitempty"`
	Hint      string `json:"hint"`
}

// PageInfo locates a page: Unit names the paged list ("days", "events", ""
// for a top-level array), From and To are the 0-based entries [From, To) of
// it on this page, Total the number of entries in all.
type PageInfo struct {
	Index int    `json:"index"`
	Of    int    `json:"of"`
	Unit  string `json:"unit,omitempty"`
	From  int    `json:"from"`
	To    int    `json:"to"`
	Total int    `json:"total"`
}

// Cut says how much of the nested lists at one path survived. Path has []
// for every list level, e.g. days[].categories[].sessions; Lists is the
// number of such lists, Kept and Total count their entries together.
type Cut struct {
	Path  string `json:"path"`
	Lists int    `json:"lists"`
	Kept  int    `json:"kept"`
	Total int    `json:"total"`
}

// Next says how to continue: pass Cursor, unchanged, as the cursor argument
// of the same call.
type Next struct {
	Cursor string `json:"cursor"`
}

// truncate bounds out to max bytes and keeps it valid JSON. It is the last
// resort for what paginate cannot split: a page whose single entry is larger
// than the bound, or a result without a list to page. A pretty-printed
// result is compacted first; what still does not fit is cut structurally,
// the deepest nested lists first, the largest among them, one entry at a
// time until it fits, then the longest text values. narrow says how to ask
// for less. A result that is not JSON is returned unchanged.
func truncate(out string, max int, narrow string) (string, *Hint) {
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
	cur, hint := cutNested(cb.Bytes(), max)
	if hint == nil {
		return string(cur), nil
	}
	hint.Truncated = true
	hint.Hint = cutText(hint, narrow)
	return string(cur), hint
}

// cutText words the hint of a cut: what is missing and how to narrow.
func cutText(h *Hint, narrow string) string {
	var parts []string
	for _, c := range h.Cut {
		parts = append(parts, c.Path+": kept "+itoa(c.Kept)+" of "+itoa(c.Total)+" entries in "+itoa(c.Lists)+" lists")
	}
	if h.Shortened > 0 {
		parts = append(parts, itoa(h.Shortened)+" long text values shortened")
	}
	msg := "output cut, sums may not match their parts: " + strings.Join(parts, "; ")
	if narrow != "" {
		return msg + "; narrow the query to see the rest: " + narrow
	}
	return msg + "; narrow the query (since, until, project, a smaller limit) to see the rest"
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
