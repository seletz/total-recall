package store

import (
	"errors"
	"fmt"
	"github.com/seletz/total-recall/internal/blobref"
	"sort"
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

// filterFixture: s1 has e1 (tool result, the picture twice) and e2 (human,
// picture and document); s2 has only text, e3. Search words: "see" is in
// e2 and e3, "build" only in e1.
func filterFixture(t *testing.T) *Store {
	t.Helper()
	s, _ := openTestStore(t)
	sess, evs := attachedEvents()
	evs[0].Text = "screenshot of the build"
	evs[1].Text = "see the picture and the report"
	sess = append(sess, model.Session{ID: "claude:s2", Harness: "claude", NativeID: "s2", Cwd: "/w/other"})
	evs = append(evs, ev("claude:e3", "claude:s2", "2026-10-31T22:02:00Z", model.ActorHuman, model.KindMessage, "user", "see the plan"))
	if _, err := s.WriteBatchOpts(ctx, SourceState{}, sess, evs, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAttachmentKind(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    AttachmentKind
		wantErr bool
	}{
		{"", AttachmentAny, false}, {"any", AttachmentAny, false}, {"image", AttachmentImage, false},
		{"document", AttachmentDocument, false}, {"video", "", true}, {"Image", "", true},
	} {
		got, err := ParseAttachmentKind(tc.in)
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("ParseAttachmentKind(%q) = %q, %v", tc.in, got, err)
		}
	}
	for _, tc := range []struct {
		kind AttachmentKind
		mt   string
		want bool
	}{
		{AttachmentAny, "image/png", true}, {AttachmentAny, "application/pdf", true},
		{AttachmentImage, "image/png", true}, {AttachmentImage, "image/jpeg", true}, {AttachmentImage, "application/pdf", false},
		{AttachmentDocument, "application/pdf", true}, {AttachmentDocument, "text/plain", true}, {AttachmentDocument, "image/png", false},
	} {
		if got := tc.kind.Matches(tc.mt); got != tc.want {
			t.Errorf("%q.Matches(%q) = %v", tc.kind, tc.mt, got)
		}
	}
}

// TestSessionsHasAttachment: the filter finds exactly the sessions with an
// attachment of the kind and actor; Attachments counts those attachments.
func TestSessionsHasAttachment(t *testing.T) {
	s := filterFixture(t)
	for _, tc := range []struct {
		name string
		f    *AttachmentFilter
		want map[string]int // session -> attachments
	}{
		{"no filter lists both, counts all", nil, map[string]int{"claude:s1": 4, "claude:s2": 0}},
		{"any", &AttachmentFilter{}, map[string]int{"claude:s1": 4}},
		{"image", &AttachmentFilter{Kind: AttachmentImage}, map[string]int{"claude:s1": 3}},
		{"document", &AttachmentFilter{Kind: AttachmentDocument}, map[string]int{"claude:s1": 1}},
		{"human", &AttachmentFilter{Actor: "human"}, map[string]int{"claude:s1": 2}},
		{"tool", &AttachmentFilter{Actor: "tool"}, map[string]int{"claude:s1": 2}},
		{"tool document", &AttachmentFilter{Kind: AttachmentDocument, Actor: "tool"}, map[string]int{}},
		{"human image", &AttachmentFilter{Kind: AttachmentImage, Actor: "human"}, map[string]int{"claude:s1": 1}},
		{"agent", &AttachmentFilter{Actor: "agent"}, map[string]int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			list, err := s.Sessions(ctx, SessionFilter{Attachment: tc.f})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]int{}
			for _, si := range list {
				got[si.ID] = si.Attachments
			}
			if len(got) != len(tc.want) {
				t.Fatalf("sessions %v, want %v", got, tc.want)
			}
			for id, n := range tc.want {
				if g, ok := got[id]; !ok || g != n {
					t.Errorf("sessions %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestSearchHasAttachment: with a word the filter keeps the event hits
// that have an attachment; without a word it lists the events with
// attachments, newest first. Every hit carries its attachments.
func TestSearchHasAttachment(t *testing.T) {
	s := filterFixture(t)
	type row struct {
		id string
		n  int
	}
	for _, tc := range []struct {
		name string
		q    TypedQuery
		want []row
	}{
		{"word without filter", TypedQuery{Text: "see"}, []row{{"claude:e2", 2}, {"claude:e3", 0}}},
		{"word with filter", TypedQuery{Text: "see", SessionFilter: SessionFilter{Attachment: &AttachmentFilter{}}}, []row{{"claude:e2", 2}}},
		{"word, other word in a tool result", TypedQuery{Text: "build", SessionFilter: SessionFilter{Attachment: &AttachmentFilter{}}}, []row{{"claude:e1", 2}}},
		{"word, image only", TypedQuery{Text: "see", SessionFilter: SessionFilter{Attachment: &AttachmentFilter{Kind: AttachmentImage}}}, []row{{"claude:e2", 1}}},
		{"word, document only", TypedQuery{Text: "see", SessionFilter: SessionFilter{Attachment: &AttachmentFilter{Kind: AttachmentDocument}}}, []row{{"claude:e2", 1}}},
		{"no word lists newest first", TypedQuery{SessionFilter: SessionFilter{Attachment: &AttachmentFilter{}}}, []row{{"claude:e2", 2}, {"claude:e1", 2}}},
		{"no word, tool", TypedQuery{Actor: "tool", SessionFilter: SessionFilter{Attachment: &AttachmentFilter{}}}, []row{{"claude:e1", 2}}},
		{"no word, document", TypedQuery{SessionFilter: SessionFilter{Attachment: &AttachmentFilter{Kind: AttachmentDocument}}}, []row{{"claude:e2", 1}}},
		{"no word, kind actor in the filter", TypedQuery{SessionFilter: SessionFilter{Attachment: &AttachmentFilter{Kind: AttachmentImage, Actor: "human"}}}, []row{{"claude:e2", 1}}},
		{"no word, limit keeps the newest", TypedQuery{SessionFilter: SessionFilter{Limit: 1, Attachment: &AttachmentFilter{}}}, []row{{"claude:e2", 2}}},
		{"no word, project scope without keys", TypedQuery{Project: &ProjectScope{}, SessionFilter: SessionFilter{Attachment: &AttachmentFilter{}}}, nil},
		{"no word, one session", TypedQuery{SessionID: "claude:s2", SessionFilter: SessionFilter{Attachment: &AttachmentFilter{}}}, nil},
		{"no word, no filter lists nothing", TypedQuery{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groups, err := s.SearchTyped(ctx, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			var got []row
			for _, h := range groups[0].Hits {
				got = append(got, row{h.Event.Event.ID, len(h.Event.Attachments)})
			}
			if tc.q.Text != "" { // best match first: the order is the rank's
				sort.Slice(got, func(i, j int) bool { return got[i].id < got[j].id })
			}
			if len(got) != len(tc.want) {
				t.Fatalf("hits %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("hits %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestSearchHasAttachmentTotalAndTypes: Total counts all matches when the
// limit cuts; the filter applies to the session type, not to memory; a
// filter of the session type without a word lists the sessions.
func TestSearchHasAttachmentTotalAndTypes(t *testing.T) {
	s := filterFixture(t)
	has := SessionFilter{Limit: 1, Attachment: &AttachmentFilter{}}
	g, err := s.SearchTyped(ctx, TypedQuery{SessionFilter: has})
	if err != nil || g[0].Total != 2 || len(g[0].Hits) != 1 {
		t.Fatalf("total = %+v, %v", g, err)
	}
	g, err = s.SearchTyped(ctx, TypedQuery{Types: []HitType{TypeSession}, SessionFilter: SessionFilter{Attachment: &AttachmentFilter{}}})
	if err != nil || len(g[0].Hits) != 1 || g[0].Hits[0].Session.ID != "claude:s1" || g[0].Hits[0].Session.Attachments != 4 {
		t.Fatalf("sessions = %+v, %v", g, err)
	}
	_, err = s.SearchTyped(ctx, TypedQuery{Types: []HitType{TypeMemory}, Text: "x", SessionFilter: SessionFilter{Attachment: &AttachmentFilter{}}})
	var fe *FilterError
	if !asFilterError(err, &fe) || fe.Filter != FilterAttachment || fe.Type != TypeMemory {
		t.Errorf("memory with has-attachment: %v", err)
	}
}

// TestAttachmentInvariants: the events with attachments are exactly the
// distinct event_ids of event_blobs, and the attachments of the hits, of
// the sessions and of the table are the same number.
func TestAttachmentInvariants(t *testing.T) {
	s := filterFixture(t)
	g, err := s.SearchTyped(ctx, TypedQuery{SessionFilter: SessionFilter{Attachment: &AttachmentFilter{}}})
	if err != nil {
		t.Fatal(err)
	}
	sum := 0
	for _, h := range g[0].Hits {
		sum += len(h.Event.Attachments)
	}
	list, err := s.Sessions(ctx, SessionFilter{Attachment: &AttachmentFilter{}})
	if err != nil {
		t.Fatal(err)
	}
	bySession := 0
	for _, si := range list {
		bySession += si.Attachments
	}
	if want := countRows(t, s, "SELECT count(DISTINCT event_id) FROM event_blobs"); len(g[0].Hits) != want {
		t.Errorf("%d hits, %d events in event_blobs", len(g[0].Hits), want)
	}
	if want := countRows(t, s, "SELECT count(*) FROM event_blobs"); sum != want || bySession != want {
		t.Errorf("attachments: hits %d, sessions %d, event_blobs %d", sum, bySession, want)
	}
}

func asFilterError(err error, target **FilterError) bool {
	return errors.As(err, target)
}

// TestSessionsProjects: Projects keeps the sessions of the raw project
// keys, together with the attachment filter.
func TestSessionsProjects(t *testing.T) {
	s := filterFixture(t)
	for _, tc := range []struct {
		name string
		f    SessionFilter
		want []string
	}{
		{"app", SessionFilter{Projects: []string{"/w/app"}}, []string{"claude:s1"}},
		{"other", SessionFilter{Projects: []string{"/w/other"}}, []string{"claude:s2"}},
		{"both", SessionFilter{Projects: []string{"/w/app", "/w/other"}}, []string{"claude:s2", "claude:s1"}},
		{"unknown", SessionFilter{Projects: []string{"/nowhere"}}, nil},
		{"with attachment", SessionFilter{Projects: []string{"/w/other"}, Attachment: &AttachmentFilter{}}, nil},
		{"app with attachment", SessionFilter{Projects: []string{"/w/app"}, Attachment: &AttachmentFilter{}}, []string{"claude:s1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			list, err := s.Sessions(ctx, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, si := range list {
				got = append(got, si.ID)
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("sessions %v, want %v", got, tc.want)
			}
		})
	}
}

// pairFixture is one session with messages made of several events of one
// transcript line (the text, then one event per image or document), like
// the demo archive: the text event shows the attachments of its message.
//
//	line 100: p#0 text with markers, p#1 image, p#2 document
//	line 200: q#0 a marker without any blob
//	line 300: r#0 plain text, at the same second as line 400
//	line 400: r2#0 text, r2#1 image
func pairFixture(t *testing.T) *Store {
	t.Helper()
	s, _ := openTestStore(t)
	sess := []model.Session{{ID: "claude:s3", Harness: "claude", NativeID: "s3", Cwd: "/w/pair"}}
	png := model.Attachment{SHA256: blobref.Hash(testPNG), MediaType: "image/png", Data: testPNG}
	pdf := model.Attachment{SHA256: blobref.Hash(testPDF), MediaType: "application/pdf", Data: testPDF}
	at := func(id, ts string, offset int64, text string, atts ...model.Attachment) model.Event {
		e := ev(id, "claude:s3", ts, model.ActorHuman, model.KindMessage, "user", text)
		e.SourcePath, e.SourceOffset, e.Attachments = "/p/s3.jsonl", offset, atts
		return e
	}
	evs := []model.Event{
		at("claude:p#0", "2026-10-31T22:00:00Z", 100, "[Image #1] [Image #2] compare the report"),
		at("claude:p#1", "2026-10-31T22:00:00Z", 100, "[image image/png 32 B]", png),
		at("claude:p#2", "2026-10-31T22:00:00Z", 100, "[document application/pdf 1.3 KiB]", pdf),
		at("claude:q#0", "2026-10-31T22:05:00Z", 200, "[Image #3] where is it"),
		at("claude:r#0", "2026-10-31T22:10:00Z", 300, "plain compare text"),
		at("claude:r2#0", "2026-10-31T22:10:00Z", 400, "[Image #1]"),
		at("claude:r2#1", "2026-10-31T22:10:00Z", 400, "[image image/png 32 B]", png),
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/p/s3.jsonl", Harness: "claude"}, sess, evs); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestAttachmentFilterPairing: the filter takes the events by the same
// pairing as EventsAttachments: the text of a message with images has
// the attachments of its line, whatever the kind asked for.
func TestAttachmentFilterPairing(t *testing.T) {
	s := pairFixture(t)
	type row struct {
		id string
		n  int
	}
	has := func(k AttachmentKind, actor string) SessionFilter {
		return SessionFilter{Attachment: &AttachmentFilter{Kind: k, Actor: actor}}
	}
	for _, tc := range []struct {
		name string
		q    TypedQuery
		want []row // newest first for a query without a word
	}{
		{"any", TypedQuery{SessionFilter: has(AttachmentAny, "")},
			[]row{{"claude:r2#1", 1}, {"claude:r2#0", 1}, {"claude:p#2", 1}, {"claude:p#1", 1}, {"claude:p#0", 2}}},
		{"image", TypedQuery{SessionFilter: has(AttachmentImage, "")},
			[]row{{"claude:r2#1", 1}, {"claude:r2#0", 1}, {"claude:p#1", 1}, {"claude:p#0", 1}}},
		{"document", TypedQuery{SessionFilter: has(AttachmentDocument, "")},
			[]row{{"claude:p#2", 1}, {"claude:p#0", 1}}},
		{"human", TypedQuery{SessionFilter: has(AttachmentAny, "human")},
			[]row{{"claude:r2#1", 1}, {"claude:r2#0", 1}, {"claude:p#2", 1}, {"claude:p#1", 1}, {"claude:p#0", 2}}},
		{"tool", TypedQuery{SessionFilter: has(AttachmentAny, "tool")}, nil},
		{"the text of the message, the way from the TUI", TypedQuery{Text: "compare", SessionFilter: has(AttachmentAny, "")},
			[]row{{"claude:p#0", 2}}},
		{"the text, images only", TypedQuery{Text: "compare", SessionFilter: has(AttachmentImage, "")}, []row{{"claude:p#0", 1}}},
		{"plain text of another line at the same second", TypedQuery{Text: "plain", SessionFilter: has(AttachmentAny, "")}, nil},
		{"a marker without a blob", TypedQuery{Text: "where", SessionFilter: has(AttachmentAny, "")}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := s.SearchTyped(ctx, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			var got []row
			for _, h := range g[0].Hits {
				got = append(got, row{h.Event.Event.ID, len(h.Event.Attachments)})
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("hits %v, want %v", got, tc.want)
			}
			if g[0].Total != len(g[0].Hits) {
				t.Errorf("total %d for %d hits", g[0].Total, len(g[0].Hits))
			}
		})
	}
}

// TestAttachmentPairingInvariants: the unique attachments (event and
// position of event_blobs) of the hits are the rows of event_blobs, so a
// pair of text and image event does not count twice; the sessions count
// the rows of the whole session.
func TestAttachmentPairingInvariants(t *testing.T) {
	s := pairFixture(t)
	for _, tc := range []struct {
		name string
		k    AttachmentKind
		q    string
	}{
		{"any", AttachmentAny, "SELECT count(*) FROM event_blobs"},
		{"image", AttachmentImage, "SELECT count(*) FROM event_blobs eb JOIN blobs b ON b.sha256 = eb.sha256 WHERE b.media_type LIKE 'image/%'"},
		{"document", AttachmentDocument, "SELECT count(*) FROM event_blobs eb JOIN blobs b ON b.sha256 = eb.sha256 WHERE b.media_type NOT LIKE 'image/%'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := countRows(t, s, tc.q)
			g, err := s.SearchTyped(ctx, TypedQuery{SessionFilter: SessionFilter{Attachment: &AttachmentFilter{Kind: tc.k}}})
			if err != nil {
				t.Fatal(err)
			}
			unique, sum := map[string]bool{}, 0
			for _, h := range g[0].Hits {
				for _, a := range h.Event.Attachments {
					unique[fmt.Sprintf("%s#%d", a.EventID, a.Position)] = true
					sum++
				}
			}
			if len(unique) != want {
				t.Errorf("%d unique attachments on the hits, %d rows in event_blobs", len(unique), want)
			}
			if sum <= want {
				t.Errorf("%d attachments in total, %d unique: the pairs should show some twice", sum, want)
			}
			list, err := s.Sessions(ctx, SessionFilter{Attachment: &AttachmentFilter{Kind: tc.k}})
			if err != nil || len(list) != 1 || list[0].Attachments != want {
				t.Errorf("sessions = %+v, %v, want one with %d attachments", list, err, want)
			}
		})
	}
}
