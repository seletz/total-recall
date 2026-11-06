package store

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

func TestParseHitTypes(t *testing.T) {
	tests := []struct {
		in      string
		want    []HitType
		wantErr string
	}{
		{"", []HitType{TypeEvent}, ""},
		{"event", []HitType{TypeEvent}, ""},
		{"memory,event", []HitType{TypeMemory, TypeEvent}, ""},
		{" session , memory,session ", []HitType{TypeSession, TypeMemory}, ""},
		{"event,,memory", []HitType{TypeEvent, TypeMemory}, ""},
		{"bogus", nil, `unknown type "bogus"`},
		{"file", nil, `type "file" is not searchable yet`},
		{"event,attachment", nil, `type "attachment" is not searchable yet`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseHitTypes(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestHitTypes(t *testing.T) {
	var names []string
	for _, ti := range HitTypes() {
		n := string(ti.Type)
		if ti.Searchable {
			n += "*"
		}
		names = append(names, n)
	}
	if got := strings.Join(names, ","); got != "event*,session*,memory*,attachment,meta,file" {
		t.Errorf("HitTypes = %s", got)
	}
}

func TestSupports(t *testing.T) {
	tests := []struct {
		t      HitType
		filter string
		want   bool
	}{
		{TypeEvent, FilterActor, true},
		{TypeEvent, FilterRaw, true},
		{TypeSession, FilterActor, false},
		{TypeSession, FilterRaw, false},
		{TypeSession, FilterSession, true},
		{TypeSession, FilterName, true},
		{TypeMemory, FilterProject, true},
		{TypeMemory, FilterHarness, true},
		{TypeMemory, FilterRaw, true},
		{TypeMemory, FilterName, false},
		{TypeMemory, FilterAccount, false},
		{TypeMemory, FilterSession, false},
		{TypeFile, FilterSince, false},
	}
	for _, tt := range tests {
		if got := Supports(tt.t, tt.filter); got != tt.want {
			t.Errorf("Supports(%s, %s) = %v, want %v", tt.t, tt.filter, got, tt.want)
		}
	}
}

// typedFixture is seed plus a subagent of claude:aaa111 in another
// directory and the memory fixture.
func typedFixture(t *testing.T, s *Store) {
	t.Helper()
	seed(t, s)
	sub := model.Session{ID: "claude:sub999", Harness: "claude", NativeID: "sub999", ParentID: "claude:aaa111", AgentName: "login helper",
		Cwd: "/w/elsewhere", StartedAt: ts("2026-10-31T22:30:00Z"), EndedAt: ts("2026-10-31T22:40:00Z")}
	evs := []model.Event{ev("claude:s1", "claude:sub999", "2026-10-31T22:31:00Z", model.ActorAgent, model.KindMessage, "assistant", "subagent checks login")}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/sub.jsonl", Harness: "claude", Offset: 1}, []model.Session{sub}, evs); err != nil {
		t.Fatal(err)
	}
	memoryFixture(t, s)
}

// hitIDs names the hits of a group: event ID, session ID or memory path.
func hitIDs(g TypeHits) string {
	var out []string
	for _, h := range g.Hits {
		switch h.Type {
		case TypeEvent:
			out = append(out, h.Event.Event.ID)
		case TypeSession:
			out = append(out, h.Session.ID)
		case TypeMemory:
			out = append(out, h.Memory.Path)
		}
	}
	return strings.Join(out, ",")
}

func TestSearchTyped(t *testing.T) {
	s, _ := openTestStore(t)
	typedFixture(t, s)
	app := ProjectRef{Key: "/w/app", Remote: "https://github.com/acme/app"}
	tests := []struct {
		name string
		q    TypedQuery
		// want per group: "type:total:ids" with ids sorted as returned
		want []string
	}{
		{"default type event", TypedQuery{Text: "login"}, []string{"event:6:"}},
		{"limit per type with total", TypedQuery{Text: "login", Types: []HitType{TypeEvent, TypeSession}, Limit: 2},
			[]string{"event:6:", "session:3:claude:sub999,claude:aaa111"}},
		{"sessions all words", TypedQuery{Text: "login FIX", Types: []HitType{TypeSession}}, []string{"session:1:claude:aaa111"}},
		{"sessions overlap since", TypedQuery{Text: "login", Types: []HitType{TypeSession}, SessionFilter: SessionFilter{Since: ts("2026-10-01T00:00:00Z")}},
			[]string{"session:2:claude:sub999,claude:aaa111"}},
		{"sessions harness", TypedQuery{Text: "login", Types: []HitType{TypeSession}, SessionFilter: SessionFilter{Harness: "other"}},
			[]string{"session:1:other:ccc333"}},
		{"session restricted to one", TypedQuery{Text: "login", Types: []HitType{TypeSession}, SessionID: "other:ccc333"},
			[]string{"session:1:other:ccc333"}},
		{"memory", TypedQuery{Text: "kompass", Types: []HitType{TypeMemory}}, []string{"memory:1:MEMORY.md"}},
		{"memory since on current version", TypedQuery{Text: "kompass", Types: []HitType{TypeMemory}, SessionFilter: SessionFilter{Since: ts("2026-10-01T00:00:00Z")}},
			[]string{"memory:0:"}},
		{"memory until", TypedQuery{Text: "beacon", Types: []HitType{TypeMemory}, SessionFilter: SessionFilter{Until: ts("2026-09-16T00:00:00Z")}},
			[]string{"memory:1:notes.md"}},
		{"memory repo", TypedQuery{Text: "kompass OR beacon", Raw: true, Types: []HitType{TypeMemory}, SessionFilter: SessionFilter{Repo: "kompass"}},
			[]string{"memory:1:MEMORY.md"}},
		{"memory harness", TypedQuery{Text: "kompass", Types: []HitType{TypeMemory}, SessionFilter: SessionFilter{Harness: "other"}},
			[]string{"memory:0:"}},
		{"groups in the order of Types", TypedQuery{Text: "docs", Types: []HitType{TypeMemory, TypeSession, TypeEvent}},
			[]string{"memory:0:", "session:1:claude:aab222", "event:2:"}},
		{"project scope events", TypedQuery{Text: "docs", Project: &ProjectScope{Keys: []string{"/w/docs"}}}, []string{"event:2:"}},
		{"project scope excludes", TypedQuery{Text: "login", Types: []HitType{TypeEvent, TypeSession}, Project: &ProjectScope{Keys: []string{"/w/docs"}}},
			[]string{"event:0:", "session:0:"}},
		{"subagent counts with its parent", TypedQuery{Text: "subagent", Project: &ProjectScope{Keys: []string{"/w/app"}}}, []string{"event:1:claude:s1"}},
		{"project scope memory", TypedQuery{Text: "kompass OR beacon", Raw: true, Types: []HitType{TypeMemory}, Project: &ProjectScope{MemoryProjectIDs: []int64{projectID(t, s, memKey2)}}},
			[]string{"memory:1:notes.md"}},
		{"empty project scope finds nothing", TypedQuery{Text: "kompass", Types: []HitType{TypeMemory, TypeEvent}, Project: &ProjectScope{}},
			[]string{"memory:0:", "event:0:"}},
		{"blank query", TypedQuery{Text: "  ", Types: []HitType{TypeEvent, TypeSession, TypeMemory}}, []string{"event:0:", "session:0:", "memory:0:"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups, err := s.SearchTyped(ctx, tt.q)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, g := range groups {
				ids := hitIDs(g)
				if g.Type == TypeEvent && len(g.Hits) > 1 {
					ids = "" // bm25 order of events is checked elsewhere
				}
				got = append(got, string(g.Type)+":"+strconv.Itoa(g.Total)+":"+ids)
				for _, h := range g.Hits {
					if h.Type != g.Type {
						t.Errorf("hit of type %s in group %s", h.Type, g.Type)
					}
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	// Hit details: core fields and the project of each type.
	groups, err := s.SearchTyped(ctx, TypedQuery{Text: "login", Types: []HitType{TypeEvent, TypeSession}, HighlightStart: "<", HighlightEnd: ">"})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range groups[0].Hits {
		if h.Event == nil || h.Session != nil || h.Memory != nil || h.TS != h.Event.Event.TS || h.Snippet != h.Event.Snippet || h.Rank != h.Event.Rank {
			t.Errorf("event hit = %+v", h)
		}
		// Every login event is in /w/app; the subagent counts with its parent.
		if h.Project != app || h.Event.Project != app {
			t.Errorf("event %s project = %+v", h.Event.Event.ID, h.Project)
		}
	}
	sess := groups[1].Hits
	if len(sess) != 3 || sess[1].Session.ID != "claude:aaa111" || sess[1].Snippet != "Fix <login>" || sess[1].TS != ts("2026-10-31T22:00:00Z") ||
		sess[1].Project != app || sess[1].Rank != 0 {
		t.Errorf("session hits = %+v", sess)
	}
	if sess[0].Snippet != "<login> helper" || sess[0].Project != app {
		t.Errorf("subagent session hit = %+v", sess[0])
	}
	if sess[2].Snippet != "<Login> audit" {
		t.Errorf("case-insensitive highlight = %q", sess[2].Snippet)
	}
	groups, _ = s.SearchTyped(ctx, TypedQuery{Text: "docs", Types: []HitType{TypeSession}})
	if h := groups[0].Hits[0]; h.Project != (ProjectRef{Key: "/w/docs", Dir: true}) {
		t.Errorf("directory project = %+v", h.Project)
	}
	groups, _ = s.SearchTyped(ctx, TypedQuery{Text: "kompass", Types: []HitType{TypeMemory}})
	if h := groups[0].Hits[0]; h.Memory == nil || h.Project != (ProjectRef{Key: "/w/kompass", Dir: true}) || h.Memory.ProjectKey != memKey ||
		h.Memory.Harness != memHarness || h.TS != ts("2026-09-30T08:00:00Z") || !strings.Contains(h.Snippet, "[kompass]") {
		t.Errorf("memory hit = %+v / %+v", h, h.Memory)
	}
	groups, _ = s.SearchTyped(ctx, TypedQuery{Text: "beacon", Types: []HitType{TypeMemory}})
	if h := groups[0].Hits[0]; h.Project != (ProjectRef{}) {
		t.Errorf("memory project without cwd = %+v", h.Project)
	}
}

func TestSearchTypedFilterErrors(t *testing.T) {
	s, _ := openTestStore(t)
	tests := []struct {
		name   string
		q      TypedQuery
		filter string
		typ    HitType
	}{
		{"actor on memory", TypedQuery{Text: "x", Types: []HitType{TypeEvent, TypeMemory}, Actor: "human"}, FilterActor, TypeMemory},
		{"raw on session", TypedQuery{Text: "x", Types: []HitType{TypeSession}, Raw: true}, FilterRaw, TypeSession},
		{"name on memory", TypedQuery{Text: "x", Types: []HitType{TypeMemory}, SessionFilter: SessionFilter{Name: "a"}}, FilterName, TypeMemory},
		{"account on memory", TypedQuery{Text: "x", Types: []HitType{TypeMemory}, SessionFilter: SessionFilter{Account: "a"}}, FilterAccount, TypeMemory},
		{"session on memory", TypedQuery{Text: "x", Types: []HitType{TypeMemory}, SessionID: "a"}, FilterSession, TypeMemory},
		{"reserved type", TypedQuery{Text: "x", Types: []HitType{TypeFile}}, "", TypeFile},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.SearchTyped(ctx, tt.q)
			var fe *FilterError
			if tt.filter == "" {
				if err == nil || errors.As(err, &fe) {
					t.Fatalf("err = %v, want a type error", err)
				}
				return
			}
			if !errors.As(err, &fe) || fe.Filter != tt.filter || fe.Type != tt.typ {
				t.Fatalf("err = %v, want filter %s on %s", err, tt.filter, tt.typ)
			}
		})
	}
}

func TestSearchProjectKeys(t *testing.T) {
	s, _ := openTestStore(t)
	typedFixture(t, s)
	hits, err := s.Search(ctx, "login", SearchFilter{ProjectKeys: []string{"/w/app"}, Actor: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Errorf("hits = %d, want a2, a3, s1", len(hits))
	}
	rows, err := s.SearchBySession(ctx, "login", SearchFilter{ProjectKeys: []string{"/w/app"}, Actor: "agent"}, OrderFirstHit)
	if err != nil || len(rows) != 2 {
		t.Errorf("by session = %+v, %v", rows, err)
	}
}
