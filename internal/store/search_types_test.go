package store

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/model"
)

// kindFixture has one event of every kind containing the word "needle",
// the picture and the document of attachedEvents (their texts hold
// "needle" too) and a file-history snapshot with two backup copies.
func kindFixture(t *testing.T) *Store {
	t.Helper()
	s, _ := openTestStore(t)
	sess, evs := attachedEvents()
	evs[0].Text = "needle screenshot of the build"
	evs[1].Text = "needle with the picture and the report"
	sess = append(sess, model.Session{ID: "claude:s2", Harness: "claude", NativeID: "s2", Cwd: "/w/other", Name: "other"})
	use := ev("claude:u1", "claude:s1", "2026-10-31T22:03:00Z", model.ActorAgent, model.KindToolUse, "assistant", "needle in a command")
	use.ToolName = "Bash"
	use2 := ev("claude:u2", "claude:s2", "2026-10-31T22:04:00Z", model.ActorAgent, model.KindToolUse, "assistant", "needle read")
	use2.ToolName = "Read"
	evs = append(evs,
		ev("claude:h1", "claude:s1", "2026-10-31T22:02:00Z", model.ActorHuman, model.KindMessage, "user", "needle typed by the human"),
		ev("claude:a1", "claude:s1", "2026-10-31T22:02:30Z", model.ActorAgent, model.KindMessage, "assistant", "needle answered by the agent"),
		use, use2,
		ev("claude:r1", "claude:s1", "2026-10-31T22:05:00Z", model.ActorTool, model.KindToolResult, "tool", "needle in the output"),
		ev("claude:p1", "claude:s2", "2026-10-31T22:06:00Z", model.ActorPeer, model.KindMessage, "user", "needle from a peer"),
	)
	old := []byte("package kompass\n\nfunc Needle() {}\n")
	cur := []byte("package kompass\n\nfunc Route() {}\n")
	meta := func(id, typ, path, at string, data []byte) model.Meta {
		h := blobref.Hash(data)
		raw := fmt.Sprintf(`{"type":%q,"trackingPath":%q,"backup":{"backupFileName":"x@v1","blob":%q}}`, typ, path, blobref.Ref{SHA256: h}.String())
		if typ == "file-history-snapshot" {
			raw = fmt.Sprintf(`{"type":%q,"snapshot":{"trackedFileBackups":{%q:{"backupFileName":"x@v1","blob":%q}}}}`, typ, path, blobref.Ref{SHA256: h}.String())
		}
		m := model.Meta{ID: id, SessionID: "claude:s1", TS: ts(at), Type: typ, SourcePath: "/src/a.jsonl", Raw: raw,
			Attachments: []model.Attachment{{SHA256: h, MediaType: "text/plain", Data: data}}}
		return m
	}
	metas := []model.Meta{
		meta("claude:f1", "file-history-snapshot", "/w/app/route.go", "2026-10-31T22:07:00Z", old),
		meta("claude:f2", "file-history-delta", "/w/app/route.go", "2026-10-31T22:08:00Z", cur),
	}
	if _, err := s.WriteBatchOpts(ctx, SourceState{}, sess, evs, WriteOptions{Meta: metas}); err != nil {
		t.Fatal(err)
	}
	return s
}

func kindIDs(g TypeHits) string {
	var out []string
	for _, h := range g.Hits {
		switch {
		case h.Event != nil:
			out = append(out, h.Event.Event.ID)
		case h.File != nil:
			out = append(out, h.File.Path)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestParseKindTypes(t *testing.T) {
	got, err := ParseHitTypes("input,answer,tool_call,tool_result,image,document,file")
	want := []HitType{TypeInput, TypeAnswer, TypeToolCall, TypeToolResult, TypeImage, TypeDocument, TypeFile}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := ParseHitTypes("attachment"); err == nil {
		t.Error("attachment must stay reserved")
	}
}

// TestSearchKindTypes: every type narrows the search to its kind of
// object; the default type event stays all of them.
func TestSearchKindTypes(t *testing.T) {
	s := kindFixture(t)
	tests := []struct {
		name string
		q    TypedQuery
		want string // type:total:ids
	}{
		{"event is all", TypedQuery{Text: "needle", Types: []HitType{TypeEvent}}, "event:8:claude:a1,claude:e1,claude:e2,claude:h1,claude:p1,claude:r1,claude:u1,claude:u2"},
		{"input", TypedQuery{Text: "needle", Types: []HitType{TypeInput}}, "input:2:claude:e2,claude:h1"},
		{"input hits only the human", TypedQuery{Text: "peer", Types: []HitType{TypeInput}}, "input:0:"},
		{"answer", TypedQuery{Text: "needle", Types: []HitType{TypeAnswer}}, "answer:1:claude:a1"},
		{"tool_call", TypedQuery{Text: "needle", Types: []HitType{TypeToolCall}}, "tool_call:2:claude:u1,claude:u2"},
		{"tool_call by tool", TypedQuery{Text: "needle", Tool: "Bash", Types: []HitType{TypeToolCall}}, "tool_call:1:claude:u1"},
		{"tool_result", TypedQuery{Text: "needle", Types: []HitType{TypeToolResult}}, "tool_result:2:claude:e1,claude:r1"},
		{"image", TypedQuery{Text: "needle", Types: []HitType{TypeImage}}, "image:2:claude:e1,claude:e2"},
		{"document", TypedQuery{Text: "needle", Types: []HitType{TypeDocument}}, "document:1:claude:e2"},
		{"image without text lists them", TypedQuery{Types: []HitType{TypeImage}}, "image:2:claude:e1,claude:e2"},
		{"document without text", TypedQuery{Types: []HitType{TypeDocument}}, "document:1:claude:e2"},
		{"image by actor of the attachment", TypedQuery{Actor: "human", Types: []HitType{TypeImage}}, "image:1:claude:e2"},
		{"input with project scope", TypedQuery{Text: "needle", Types: []HitType{TypeInput}, Project: &ProjectScope{Keys: []string{"/w/other"}}}, "input:0:"},
		{"input in a session", TypedQuery{Text: "needle", Types: []HitType{TypeInput}, SessionID: "claude:s2"}, "input:0:"},
		{"file by content word", TypedQuery{Text: "needle", Types: []HitType{TypeFile}}, "file:1:/w/app/route.go"},
		{"file all words, any case", TypedQuery{Text: "FUNC kompass", Types: []HitType{TypeFile}}, "file:2:/w/app/route.go,/w/app/route.go"},
		{"file no match", TypedQuery{Text: "absent", Types: []HitType{TypeFile}}, "file:0:"},
		{"file in a session", TypedQuery{Text: "func", Types: []HitType{TypeFile}, SessionID: "claude:s2"}, "file:0:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups, err := s.SearchTyped(ctx, tt.q)
			if err != nil {
				t.Fatal(err)
			}
			g := groups[0]
			if got := fmt.Sprintf("%s:%d:%s", g.Type, g.Total, kindIDs(g)); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
			for _, h := range g.Hits {
				if h.Type != g.Type {
					t.Errorf("hit type %s in group %s", h.Type, g.Type)
				}
			}
		})
	}
}

// TestSearchFileHit: a file hit carries path, session, version time and
// hash, and a snippet around the first word.
func TestSearchFileHit(t *testing.T) {
	s := kindFixture(t)
	groups, err := s.SearchTyped(ctx, TypedQuery{Text: "needle", Types: []HitType{TypeFile}})
	if err != nil || len(groups[0].Hits) != 1 {
		t.Fatalf("%v, %v", groups, err)
	}
	h := groups[0].Hits[0]
	f := h.File
	if f.Path != "/w/app/route.go" || f.SessionID != "claude:s1" || f.MetaID != "claude:f1" || f.MediaType != "text/plain" ||
		f.SHA256 != blobref.Hash([]byte("package kompass\n\nfunc Needle() {}\n")) || f.Bytes == 0 {
		t.Errorf("file = %+v", f)
	}
	if !strings.Contains(h.Snippet, "[Needle]") || !h.TS.Equal(ts("2026-10-31T22:07:00Z")) || h.Project.Key != "/w/app" {
		t.Errorf("hit = %+v", h)
	}
}

// TestSearchKindFilters: filters a type does not apply are refused, the
// ones it applies work.
func TestSearchKindFilters(t *testing.T) {
	tests := []struct {
		t      HitType
		filter string
		want   bool
	}{
		{TypeInput, FilterActor, false},
		{TypeAnswer, FilterActor, false},
		{TypeToolCall, FilterActor, false},
		{TypeToolCall, FilterTool, true},
		{TypeToolResult, FilterTool, false},
		{TypeEvent, FilterTool, false},
		{TypeInput, FilterRaw, true},
		{TypeInput, FilterAttachment, true},
		{TypeImage, FilterAttachment, false},
		{TypeImage, FilterActor, true},
		{TypeDocument, FilterRaw, true},
		{TypeFile, FilterRaw, false},
		{TypeFile, FilterActor, false},
		{TypeFile, FilterProject, true},
		{TypeFile, FilterSession, true},
		{TypeFile, FilterAttachment, false},
	}
	for _, tt := range tests {
		if got := Supports(tt.t, tt.filter); got != tt.want {
			t.Errorf("Supports(%s, %s) = %v, want %v", tt.t, tt.filter, got, tt.want)
		}
	}
	s := kindFixture(t)
	if _, err := s.SearchTyped(ctx, TypedQuery{Text: "x", Actor: "human", Types: []HitType{TypeInput}}); err == nil {
		t.Error("actor with input accepted")
	}
}

// TestSearchKindsAddUp: the kinds partition the events (plus the
// attachment types, which overlap), so the typed counts of the
// partitioning types add up to the count of event.
func TestSearchKindsAddUp(t *testing.T) {
	s := kindFixture(t)
	groups, err := s.SearchTyped(ctx, TypedQuery{Text: "needle", Types: []HitType{TypeEvent, TypeInput, TypeAnswer, TypeToolCall, TypeToolResult}})
	if err != nil {
		t.Fatal(err)
	}
	sum := 0
	for _, g := range groups[1:] {
		sum += g.Total
	}
	// Event 8 = input 2 (h1 and the message e2 with attachments) +
	// answer 1 + tool_call 2 + tool_result 2 + the peer message.
	if groups[0].Total != 8 || sum != 7 {
		t.Errorf("event %d, kinds %d", groups[0].Total, sum)
	}
}

// TestSearchKindLimit: the limit cuts every group, Total counts all.
func TestSearchKindLimit(t *testing.T) {
	s := kindFixture(t)
	for _, tt := range []struct {
		t     HitType
		text  string
		total int
	}{{TypeToolCall, "needle", 2}, {TypeFile, "func", 2}, {TypeImage, "needle", 2}, {TypeInput, "needle", 2}} {
		groups, err := s.SearchTyped(ctx, TypedQuery{Text: tt.text, Types: []HitType{tt.t}, SessionFilter: SessionFilter{Limit: 1}})
		if err != nil || len(groups[0].Hits) != 1 || groups[0].Total != tt.total {
			t.Errorf("%s: %d hits, total %d, %v", tt.t, len(groups[0].Hits), groups[0].Total, err)
		}
	}
}
