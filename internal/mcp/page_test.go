package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// statsLike builds a worktime-like document: totals, days with categories
// that have projects, refs and sessions, and a month-wide category list.
// Every sum is consistent: a day is the sum of its categories, a category
// the sum of its projects, the total the sum of the days.
func statsLike(days, cats, projects int) string {
	var d []string
	total := 0
	for i := 0; i < days; i++ {
		var c []string
		day := 0
		for j := 0; j < cats; j++ {
			var p []string
			cat := 0
			for k := 0; k < projects; k++ {
				n := 60 * (k + 1)
				cat += n
				p = append(p, fmt.Sprintf(`{"project":"p%d","key":"/src/p%d","raw_seconds":%d,"refs":["#%d","#%d"]}`, k, k, n, k, k+1))
			}
			day += cat
			c = append(c, fmt.Sprintf(`{"category":"c%d","raw_seconds":%d,"projects":[%s],"sessions":[{"id":"claude:%08d-aaaa","title":"t<&>","raw_seconds":%d}],"text":"c%d: text"}`,
				j, cat, strings.Join(p, ","), j, cat, j))
		}
		total += day
		d = append(d, fmt.Sprintf(`{"day":"2026-10-%02d","raw_seconds":%d,"categories":[%s]}`, i+1, day, strings.Join(c, ",")))
	}
	return fmt.Sprintf(`{"timezone":"Europe/Berlin","days":[%s],"categories":[{"category":"c0","raw_seconds":%d,"days":%d}],"total":{"raw_seconds":%d,"days":%d}}`,
		strings.Join(d, ","), total, days, total, days)
}

func rootList(n int) string {
	var s []string
	for i := 0; i < n; i++ {
		s = append(s, fmt.Sprintf(`{"id":"claude:%08d","snippet":"a <b> & c %d"}`, i, i))
	}
	return "[" + strings.Join(s, ",") + "]"
}

func eventsDoc(n int) string {
	var s []string
	for i := 0; i < n; i++ {
		s = append(s, fmt.Sprintf(`{"n":%d,"text":"event %d"}`, i, i))
	}
	return `{"session":{"id":"claude:1","events":` + fmt.Sprint(n) + `},"events":[` + strings.Join(s, ",") + `]}`
}

func pagedServer(doc string, units []string, max int) *Server {
	return &Server{Name: "t", Version: "test", MaxBytes: max,
		Run: func(context.Context, []string) (string, bool) { return doc, false },
		Tools: []Tool{{Name: "x", Command: []string{"x"}, FixedFlags: []string{"--json"}, Units: units,
			Params: []Param{{Name: "cursor", Type: "string", Server: true}}}}}
}

// callPage calls tool x with the cursor and returns the result.
func callPage(t *testing.T, s *Server, cursor string) (first string, hint Hint, isErr bool, all []string) {
	t.Helper()
	args := `{}`
	if cursor != "" {
		args = fmt.Sprintf(`{"cursor":%q}`, cursor)
	}
	m := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":`+args+`}}`)
	res := m["result"].(map[string]any)
	for _, c := range res["content"].([]any) {
		all = append(all, c.(map[string]any)["text"].(string))
	}
	if len(all) == 2 {
		if err := json.Unmarshal([]byte(all[1]), &hint); err != nil {
			t.Fatalf("hint: %v: %s", err, all[1])
		}
	}
	return all[0], hint, res["isError"].(bool), all
}

// crawl follows next.cursor from the first page and returns all pages.
func crawl(t *testing.T, s *Server) (pages []string, hints []Hint) {
	t.Helper()
	cursor := ""
	for i := 0; i < 1000; i++ {
		first, h, isErr, _ := callPage(t, s, cursor)
		if isErr {
			t.Fatalf("page %d: error %s", i, first)
		}
		pages = append(pages, first)
		hints = append(hints, h)
		if h.Next == nil {
			return pages, hints
		}
		cursor = h.Next.Cursor
	}
	t.Fatal("pages do not end")
	return nil, nil
}

// splice rebuilds the full document from pages: the page one with the
// unit lists of all pages in place of its own.
func splice(t *testing.T, pages []string, unit string) string {
	t.Helper()
	var first map[string]json.RawMessage
	var units []json.RawMessage
	for i, p := range pages {
		var m map[string]json.RawMessage
		if unit == "" {
			var l []json.RawMessage
			if err := json.Unmarshal([]byte(p), &l); err != nil {
				t.Fatal(err)
			}
			units = append(units, l...)
			continue
		}
		if err := json.Unmarshal([]byte(p), &m); err != nil {
			t.Fatal(err)
		}
		var l []json.RawMessage
		if err := json.Unmarshal(m[unit], &l); err != nil {
			t.Fatal(err)
		}
		units = append(units, l...)
		if i == 0 {
			first = m
		}
	}
	var b bytes.Buffer
	if unit == "" {
		b.WriteByte('[')
		for i, u := range units {
			if i > 0 {
				b.WriteByte(',')
			}
			b.Write(u)
		}
		b.WriteByte(']')
		return b.String()
	}
	b.WriteByte('{')
	for i, k := range orderedKeys([]byte(pages[0])) {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		if k == unit {
			b.WriteByte('[')
			for j, u := range units {
				if j > 0 {
					b.WriteByte(',')
				}
				b.Write(u)
			}
			b.WriteByte(']')
		} else {
			b.Write(first[k])
		}
	}
	b.WriteByte('}')
	return b.String()
}

func compact(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(s)); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestPagesReassembleByteIdentical(t *testing.T) {
	for _, tc := range []struct {
		name  string
		doc   string
		units []string
		unit  string
		max   int
	}{
		{"stats days", statsLike(12, 4, 3), []string{"days", "buckets"}, "days", 4000},
		{"stats days, many pages", statsLike(30, 5, 4), []string{"days"}, "days", 6000},
		{"buckets are the second candidate", strings.Replace(statsLike(10, 3, 2), `"days":[`, `"buckets":[`, 1), []string{"days", "buckets"}, "buckets", 3000},
		{"root list", rootList(400), []string{""}, "", 3000},
		{"events", eventsDoc(300), []string{"events"}, "events", 1500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := compact(t, tc.doc)
			if len(want) < 3*tc.max {
				t.Fatalf("fixture too small: %d bytes, max %d", len(want), tc.max)
			}
			pages, hints := crawl(t, pagedServer(tc.doc, tc.units, tc.max))
			if len(pages) < 3 {
				t.Fatalf("%d pages, want several", len(pages))
			}
			for i, p := range pages {
				if len(p) > tc.max {
					t.Errorf("page %d is %d bytes, max %d", i, len(p), tc.max)
				}
				if !json.Valid([]byte(p)) {
					t.Errorf("page %d is not JSON", i)
				}
				h := hints[i]
				if h.Truncated || h.Page == nil || h.Page.Index != i+1 || h.Page.Of != len(pages) || h.Hint == "" {
					t.Errorf("page %d: hint %+v", i, h)
				}
				if (i == len(pages)-1) != (h.Next == nil) {
					t.Errorf("page %d of %d: next = %+v", i+1, len(pages), h.Next)
				}
			}
			if got := splice(t, pages, tc.unit); got != want {
				t.Errorf("pages do not add up to the CLI JSON\n got: %.200s\nwant: %.200s", got, want)
			}
		})
	}
}

// TestPageTotalsOnEveryPage checks the invariants of the reported bug on
// every page: the totals are the same everywhere and the parts of a page
// add up (day = sum of its categories, category = sum of its projects).
func TestPageTotalsOnEveryPage(t *testing.T) {
	doc := statsLike(31, 6, 5)
	pages, _ := crawl(t, pagedServer(doc, []string{"days"}, 7000))
	if len(pages) < 4 {
		t.Fatalf("%d pages", len(pages))
	}
	type page struct {
		Total      json.RawMessage `json:"total"`
		Categories json.RawMessage `json:"categories"`
		Days       []struct {
			Day        string
			Raw        int `json:"raw_seconds"`
			Categories []struct {
				Raw      int `json:"raw_seconds"`
				Projects []struct {
					Raw  int `json:"raw_seconds"`
					Refs []string
				}
			}
		}
	}
	var full page
	if err := json.Unmarshal([]byte(doc), &full); err != nil {
		t.Fatal(err)
	}
	sum, seen := 0, 0
	for i, p := range pages {
		var pg page
		if err := json.Unmarshal([]byte(p), &pg); err != nil {
			t.Fatal(err)
		}
		if string(pg.Total) != string(full.Total) || string(pg.Categories) != string(full.Categories) {
			t.Errorf("page %d: totals differ from the whole", i+1)
		}
		for _, d := range pg.Days {
			c := 0
			for _, cat := range d.Categories {
				pr := 0
				for _, pj := range cat.Projects {
					pr += pj.Raw
					if len(pj.Refs) != 2 {
						t.Errorf("%s: refs cut", d.Day)
					}
				}
				if pr != cat.Raw || len(cat.Projects) != 5 {
					t.Errorf("%s: category %d != projects %d (%d projects)", d.Day, cat.Raw, pr, len(cat.Projects))
				}
				c += cat.Raw
			}
			if c != d.Raw || len(d.Categories) != 6 {
				t.Errorf("%s: day %d != categories %d (%d categories)", d.Day, d.Raw, c, len(d.Categories))
			}
			sum += d.Raw
			seen++
		}
	}
	var tot struct{ Raw int `json:"raw_seconds"` }
	_ = json.Unmarshal(full.Total, &tot)
	if seen != 31 || sum != tot.Raw {
		t.Errorf("%d days, sum %d, total %d", seen, sum, tot.Raw)
	}
}

// TestPageListLargerThanPage is the case in which the top list itself does
// not fit: each unit is a good fraction of a page.
func TestPageListLargerThanPage(t *testing.T) {
	doc := statsLike(8, 6, 6) // a day is a few KiB
	one := len(compact(t, doc))/8 + 1
	max := 2*one + 600 // two days and the rest at most
	pages, _ := crawl(t, pagedServer(doc, []string{"days"}, max))
	if len(pages) < 3 || len(pages) > 8 {
		t.Errorf("%d pages for 8 days", len(pages))
	}
	if got := splice(t, pages, "days"); got != compact(t, doc) {
		t.Errorf("pages do not add up")
	}
}

func TestPageOversizeUnitIsFlagged(t *testing.T) {
	doc := statsLike(3, 8, 8)
	s := pagedServer(doc, []string{"days"}, 1500) // a day alone is larger
	first, h, isErr, _ := callPage(t, s, "")
	if isErr || len(first) > 1500 || !json.Valid([]byte(first)) || !h.Truncated || len(h.Cut) == 0 {
		t.Errorf("first=%d bytes err=%v hint=%+v", len(first), isErr, h)
	}
}

func TestPageSmallOutputUnchanged(t *testing.T) {
	doc := statsLike(1, 1, 1)
	first, h, _, all := callPage(t, pagedServer(doc, []string{"days"}, 100000), "")
	if first != doc || h.Hint != "" || len(all) != 1 {
		t.Errorf("small output changed: %d items", len(all))
	}
}

func TestPageCursorErrors(t *testing.T) {
	doc := statsLike(20, 4, 3)
	s := pagedServer(doc, []string{"days"}, 4000)
	_, h, _, _ := callPage(t, s, "")
	good := h.Next.Cursor
	stale := pagedServer(strings.Replace(doc, `"timezone":"Europe/Berlin"`, `"timezone":"UTC"`, 1), []string{"days"}, 4000)
	for _, tc := range []struct {
		name   string
		s      *Server
		cursor string
		want   string
	}{
		{"malformed", s, "nonsense", "cursor"},
		{"offset is not a page start", s, "3-" + strings.SplitN(good, "-", 2)[1], "cursor"},
		{"beyond the end", s, "999-" + strings.SplitN(good, "-", 2)[1], "cursor"},
		{"archive changed", stale, good, "changed"},
		{"no pages", pagedServer(`{"a":1}`, []string{"days"}, 4000), good, "no pages"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, _, isErr, _ := callPage(t, tc.s, tc.cursor)
			if !isErr || !strings.Contains(first, `"kind":"usage"`) || !strings.Contains(first, tc.want) {
				t.Errorf("isError=%v: %s", isErr, first)
			}
		})
	}
}

func TestPageShowKeepsOffset(t *testing.T) {
	s := pagedServer(eventsDoc(300), []string{"events"}, 1500)
	s.Tools[0].Offset = "offset"
	s.Tools[0].Params = append(s.Tools[0].Params, Param{Name: "offset", Type: "integer"})
	m := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{"offset":1000}}}`)
	var h Hint
	text := m["result"].(map[string]any)["content"].([]any)[1].(map[string]any)["text"].(string)
	if err := json.Unmarshal([]byte(text), &h); err != nil || h.Next == nil || h.Next.Offset <= 1000 || h.Next.Cursor == "" {
		t.Errorf("next = %+v, %v", h.Next, err)
	}
}
