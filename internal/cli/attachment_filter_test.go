package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// attachEnv is the CLI against the demo archive, whose only attachments
// are an invented image and PDF in two human events of one session.
func attachEnv(t *testing.T) (*testEnv, []string) {
	t.Helper()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	return e, []string{"--db", db}
}

func (e *testEnv) json(t *testing.T, base []string, args ...string) string {
	t.Helper()
	r := e.run(t, "", append(append(append([]string{}, base...), args...), "--json")...)
	if r.code != 0 {
		t.Fatalf("%v: exit %d: %s %s", args, r.code, r.stdout, r.stderr)
	}
	return r.stdout
}

func sqlCount(t *testing.T, e *testEnv, base []string, q string) int {
	t.Helper()
	rows := decode[[]map[string]float64](t, e.json(t, base, "sql", q))
	for _, v := range rows[0] {
		return int(v)
	}
	t.Fatal("no value")
	return 0
}

// TestSearchHasAttachment: without a word the newest events with
// attachments, with a word the hits that have one; every event hit
// carries its attachments and their number. The demo archive has one
// message with an image and a document: the text event and one event per
// attachment, all at the same moment. The filter finds the text too, with
// the attachments of its message, as the viewer does.
func TestSearchHasAttachment(t *testing.T) {
	e, base := attachEnv(t)
	const text, img, doc = "claude:e00558b#0", "claude:e00558b#1", "claude:e00558b#2"
	all := []string{doc, img, text}
	for _, tc := range []struct {
		name  string
		args  []string
		want  []string // event ids, in output order
		count []int
	}{
		{"without a word", []string{"search", "--has-attachment"}, all, []int{1, 1, 2}},
		{"any", []string{"search", "--has-attachment=any"}, all, []int{1, 1, 2}},
		{"image", []string{"search", "--has-attachment=image"}, []string{img, text}, []int{1, 1}},
		{"document", []string{"search", "--has-attachment=document"}, []string{doc, text}, []int{1, 1}},
		{"word of the placeholder", []string{"search", "image", "--has-attachment"}, []string{img}, []int{1}},
		{"word of the placeholder, other kind", []string{"search", "image", "--has-attachment=document"}, nil, nil},
		{"word of the text of the message", []string{"search", "screenshot", "--has-attachment"}, []string{text}, []int{2}},
		{"word of the text, images only", []string{"search", "screenshot", "--has-attachment=image"}, []string{text}, []int{1}},
		{"human", []string{"search", "--has-attachment", "--actor", "human"}, all, []int{1, 1, 2}},
		{"tool", []string{"search", "--has-attachment", "--actor", "tool"}, nil, nil},
		{"project", []string{"search", "--has-attachment", "--project", "kompass"}, all, []int{1, 1, 2}},
		{"other project", []string{"search", "--has-attachment", "--project", "wetterwarte"}, nil, nil},
		{"window with the events", []string{"search", "--has-attachment", "--since", "2026-10-28", "--until", "2026-10-28"}, all, []int{1, 1, 2}},
		{"window without", []string{"search", "--has-attachment", "--since", "2026-10-29"}, nil, nil},
		{"limit keeps the newest", []string{"search", "--has-attachment", "--limit", "1"}, []string{doc}, []int{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits := decode[[]HitJSON](t, e.json(t, base, tc.args...))
			if len(hits) != len(tc.want) {
				t.Fatalf("%d hits, want %v: %+v", len(hits), tc.want, hits)
			}
			for i, h := range hits {
				if h.EventID != tc.want[i] || h.AttachmentCount != tc.count[i] || len(h.Attachments) != tc.count[i] || h.Type != "event" {
					t.Errorf("hit %d = %+v, want %s with %d attachments", i, h, tc.want[i], tc.count[i])
				}
			}
		})
	}
}

// TestSearchWithoutFilterHasCounts: a plain search is unchanged, and every
// hit says how many attachments the event has (0 for most).
func TestSearchWithoutFilterHasCounts(t *testing.T) {
	e, base := attachEnv(t)
	hits := decode[[]HitJSON](t, e.json(t, base, "search", "image", "--limit", "0"))
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	with := 0
	for _, h := range hits {
		if h.AttachmentCount != len(h.Attachments) || h.Attachments == nil {
			t.Errorf("hit %s: count %d, list %v", h.EventID, h.AttachmentCount, h.Attachments)
		}
		if h.AttachmentCount > 0 {
			with++
		}
	}
	filtered := decode[[]HitJSON](t, e.json(t, base, "search", "image", "--has-attachment", "--limit", "0"))
	if len(filtered) != with {
		t.Errorf("the filter keeps %d hits, %d have attachments", len(filtered), with)
	}
}

// TestSearchAttachmentTypeAndErrors: the filter applies to the types event
// and session, not to memory; a search needs a word or the filter.
func TestSearchAttachmentTypeAndErrors(t *testing.T) {
	e, base := attachEnv(t)
	ss := decode[[]SessionSearchHitJSON](t, e.json(t, base, "search", "--has-attachment", "--type", "session"))
	if len(ss) != 1 || ss[0].AttachmentCount != 2 || ss[0].Type != "session" {
		t.Errorf("session hits = %+v", ss)
	}
	for _, tc := range []struct {
		name string
		args []string
		msg  string
	}{
		{"memory", []string{"search", "x", "--has-attachment", "--type", "memory"}, "--has-attachment does not apply to type memory"},
		{"bad kind", []string{"search", "--has-attachment=video"}, "invalid --has-attachment"},
		{"neither word nor filter", []string{"search"}, "needs a query or --has-attachment"},
		{"by-session without a word", []string{"search", "--has-attachment", "--by-session"}, "--by-session needs a query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := e.run(t, "", append(append(append([]string{}, base...), tc.args...), "--json")...)
			if r.code != 1 || !strings.Contains(r.stdout, `"kind":"usage"`) || !strings.Contains(r.stdout, tc.msg) {
				t.Errorf("exit %d: %s", r.code, r.stdout)
			}
		})
	}
	// With a word, by-session groups the hits that have an attachment.
	rows := decode[[]SessionHitsJSON](t, e.json(t, base, "search", "image", "--has-attachment", "--by-session"))
	if len(rows) != 1 || rows[0].Hits != 1 {
		t.Errorf("by-session = %+v", rows)
	}
}

// TestSessionsHasAttachment: the filter finds the sessions with an
// attachment of the kind and actor and counts them.
func TestSessionsHasAttachment(t *testing.T) {
	e, base := attachEnv(t)
	for _, tc := range []struct {
		name string
		args []string
		want int // attachments of the one session, -1 = no session
	}{
		{"any", []string{"--has-attachment"}, 2},
		{"image", []string{"--has-attachment=image"}, 1},
		{"document", []string{"--has-attachment=document"}, 1},
		{"human", []string{"--has-attachment", "--actor", "human"}, 2},
		{"tool", []string{"--has-attachment", "--actor", "tool"}, -1},
		{"project", []string{"--has-attachment", "--project", "kompass"}, 2},
		{"other project", []string{"--has-attachment", "--project", "wetterwarte"}, -1},
		{"window", []string{"--has-attachment", "--since", "2026-10-28", "--until", "2026-10-28"}, 2},
		{"other window", []string{"--has-attachment", "--since", "2026-10-20", "--until", "2026-10-21"}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ss := decode[[]SessionJSON](t, e.json(t, base, append([]string{"sessions"}, tc.args...)...))
			switch {
			case tc.want < 0 && len(ss) != 0:
				t.Errorf("sessions = %d, want none", len(ss))
			case tc.want >= 0 && (len(ss) != 1 || ss[0].AttachmentCount != tc.want):
				t.Errorf("sessions = %+v, want one with %d attachments", ss, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name string
		args []string
		msg  string
	}{
		{"actor alone", []string{"sessions", "--actor", "human"}, "--actor needs --has-attachment"},
		{"bad kind", []string{"sessions", "--has-attachment=video"}, "invalid --has-attachment"},
		{"bad actor", []string{"sessions", "--has-attachment", "--actor", "bogus"}, "invalid --actor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := e.run(t, "", append(append(append([]string{}, base...), tc.args...), "--json")...)
			if r.code != 1 || !strings.Contains(r.stdout, `"kind":"usage"`) || !strings.Contains(r.stdout, tc.msg) {
				t.Errorf("exit %d: %s", r.code, r.stdout)
			}
		})
	}
}

// TestAttachmentInvariants (real --json output against the tables): the
// unique attachments of the hits (the event they are linked to and the
// position, as in event_blobs) are the rows of event_blobs, in total, per
// kind and as a sum of days; a pair of text and image event does not
// count twice. The sessions count the same rows, with and without the
// filter.
func TestAttachmentInvariants(t *testing.T) {
	e, base := attachEnv(t)
	events := sqlCount(t, e, base, "SELECT count(DISTINCT event_id) FROM event_blobs")
	rows := sqlCount(t, e, base, "SELECT count(*) FROM event_blobs")
	images := sqlCount(t, e, base, "SELECT count(*) FROM event_blobs eb JOIN blobs b ON b.sha256 = eb.sha256 WHERE b.media_type LIKE 'image/%'")
	if events == 0 || rows == 0 || images == 0 || images == rows {
		t.Fatalf("demo archive: %d events, %d attachments, %d images", events, rows, images)
	}
	// hitAttachments returns the hits, the unique attachments on them and
	// the attachments counted per hit.
	hitAttachments := func(args ...string) (hits, unique, sum int) {
		seen := map[string]bool{}
		for _, h := range decode[[]HitJSON](t, e.json(t, base, append([]string{"search", "--has-attachment", "--limit", "0"}, args...)...)) {
			hits++
			sum += h.AttachmentCount
			if h.AttachmentCount != len(h.Attachments) || h.AttachmentCount == 0 {
				t.Errorf("hit %s: count %d, list %d", h.EventID, h.AttachmentCount, len(h.Attachments))
			}
			for _, a := range h.Attachments {
				if a.EventID == "" {
					t.Errorf("hit %s: attachment without event_id", h.EventID)
				}
				seen[fmt.Sprintf("%s#%d", a.EventID, a.Position)] = true
			}
		}
		return hits, len(seen), sum
	}
	sessionAttachments := func(args ...string) (n, sum int) {
		for _, s := range decode[[]SessionJSON](t, e.json(t, base, append([]string{"sessions", "--subagents"}, args...)...)) {
			n++
			sum += s.AttachmentCount
		}
		return n, sum
	}
	h, unique, sum := hitAttachments()
	if unique != rows {
		t.Errorf("unique attachments on the hits: %d, rows of event_blobs %d", unique, rows)
	}
	if h < events || sum <= rows {
		t.Errorf("the demo archive has a message of text and attachment events: %d hits for %d events, %d attachments counted for %d rows", h, events, sum, rows)
	}
	if _, unique, _ := hitAttachments("--has-attachment=image"); unique != images {
		t.Errorf("images: %d, table %d", unique, images)
	}
	if _, unique, _ := hitAttachments("--has-attachment=document"); unique != rows-images {
		t.Errorf("documents: %d, table %d", unique, rows-images)
	}
	if _, sum := sessionAttachments("--has-attachment"); sum != rows {
		t.Errorf("sessions with the filter: %d attachments, table %d", sum, rows)
	}
	// The same through the event window, as a sum of days.
	byDay := 0
	for _, day := range []string{"2026-10-26", "2026-10-27", "2026-10-28", "2026-10-29"} {
		_, unique, _ := hitAttachments("--since", day, "--until", day)
		byDay += unique
	}
	if byDay != rows {
		t.Errorf("attachments by day: %d, table %d", byDay, rows)
	}
	// Without the filter: every session, the same attachments in total.
	all := decode[[]SessionJSON](t, e.json(t, base, "sessions", "--subagents"))
	n, sum := sessionAttachments()
	if n != len(all) || sum != rows {
		t.Errorf("sessions without the filter: %d sessions, %d attachments, table %d", n, sum, rows)
	}
	if len(all) == 1 {
		t.Error("the demo archive has only one session")
	}
}
