package store

import (
	"errors"
	"fmt"
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
