package store

import (
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

// seedOwners writes one account in two organizations (team and solo), a
// second account, a subagent of the solo session without owner of its own
// and a session without any owner:
//
//	t   acc-1/org-team   (human, agent)
//	s   acc-1/org-solo   (human)
//	ssub subagent of s   (agent)
//	p   acc-2/org-priv   (human, agent)
//	n   no owner         (human)
func seedOwners(t *testing.T, s *Store) {
	t.Helper()
	sessions := []model.Session{
		{ID: "claude:t", Harness: "claude", NativeID: "t", Name: "T", Cwd: "/w", AccountID: "acc-1", OrganizationID: "org-team",
			StartedAt: ts("2026-10-01T10:00:00Z"), EndedAt: ts("2026-10-01T11:00:00Z")},
		{ID: "claude:s", Harness: "claude", NativeID: "s", Name: "S", Cwd: "/w", AccountID: "acc-1", OrganizationID: "org-solo",
			StartedAt: ts("2026-10-02T10:00:00Z"), EndedAt: ts("2026-10-02T11:00:00Z")},
		{ID: "claude:ssub", Harness: "claude", NativeID: "ssub", ParentID: "claude:s", Cwd: "/w",
			StartedAt: ts("2026-10-02T10:05:00Z"), EndedAt: ts("2026-10-02T10:06:00Z")},
		{ID: "claude:p", Harness: "claude", NativeID: "p", Name: "P", Cwd: "/w", AccountID: "acc-2", OrganizationID: "org-priv",
			StartedAt: ts("2026-10-03T10:00:00Z"), EndedAt: ts("2026-10-03T11:00:00Z")},
		{ID: "claude:n", Harness: "claude", NativeID: "n", Name: "N", Cwd: "/w",
			StartedAt: ts("2026-10-04T10:00:00Z"), EndedAt: ts("2026-10-04T11:00:00Z")},
	}
	evs := []model.Event{
		ev("claude:t-1", "claude:t", "2026-10-01T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "searchword t"),
		ev("claude:t-2", "claude:t", "2026-10-01T10:01:00Z", model.ActorAgent, model.KindMessage, "assistant", "t2"),
		ev("claude:s-1", "claude:s", "2026-10-02T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "searchword s"),
		ev("claude:ss-1", "claude:ssub", "2026-10-02T10:05:00Z", model.ActorAgent, model.KindMessage, "assistant", "searchword ss"),
		ev("claude:p-1", "claude:p", "2026-10-03T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "p"),
		ev("claude:p-2", "claude:p", "2026-10-03T10:01:00Z", model.ActorAgent, model.KindMessage, "assistant", "p2"),
		ev("claude:n-1", "claude:n", "2026-10-04T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "n"),
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/o.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
}

var (
	ownTeam = Owner{"acc-1", "org-team"}
	ownSolo = Owner{"acc-1", "org-solo"}
	ownPriv = Owner{"acc-2", "org-priv"}
)

func TestOwners(t *testing.T) {
	s, _ := openTestStore(t)
	seedOwners(t, s)
	got, err := s.Owners(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []Owner{{}, ownSolo, ownTeam, ownPriv}
	if len(got) != len(want) {
		t.Fatalf("Owners = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Owners = %v, want %v", got, want)
		}
	}
}

func TestSessionsOwnerFilter(t *testing.T) {
	s, _ := openTestStore(t)
	seedOwners(t, s)
	tests := []struct {
		name string
		f    SessionFilter
		want string
	}{
		{"one pair", SessionFilter{Owners: &OwnerSet{[]Owner{ownTeam}}}, "claude:t"},
		{"both orgs of one account, subagent inherits", SessionFilter{Owners: &OwnerSet{[]Owner{ownTeam, ownSolo}}}, "claude:ssub,claude:s,claude:t"},
		{"empty set", SessionFilter{Owners: &OwnerSet{}}, ""},
		{"nil does not filter", SessionFilter{}, "claude:n,claude:p,claude:ssub,claude:s,claude:t"},
		{"account prefix covers the subagent", SessionFilter{Account: "acc-1"}, "claude:ssub,claude:s,claude:t"},
		{"account and owners", SessionFilter{Account: "acc-1", Owners: &OwnerSet{[]Owner{ownTeam, ownPriv}}}, "claude:t"},
		{"no owner at all", SessionFilter{Owners: &OwnerSet{[]Owner{{}}}}, "claude:n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list, err := s.Sessions(ctx, tt.f)
			if err != nil {
				t.Fatal(err)
			}
			if got := ids(list); got != tt.want {
				t.Errorf("Sessions = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionInfoOwner(t *testing.T) {
	s, _ := openTestStore(t)
	seedOwners(t, s)
	for id, want := range map[string]Owner{"claude:t": ownTeam, "claude:ssub": ownSolo, "claude:n": {}} {
		si, err := s.Session(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got := si.Owner(); got != want {
			t.Errorf("%s: Owner = %v, want %v", id, got, want)
		}
	}
}

func TestSearchOwnerFilter(t *testing.T) {
	s, _ := openTestStore(t)
	seedOwners(t, s)
	hits, err := s.Search(ctx, "searchword", SearchFilter{SessionFilter: SessionFilter{Owners: &OwnerSet{[]Owner{ownSolo}}}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, h := range hits {
		got[h.Event.SessionID] = true
	}
	if len(got) != 2 || !got["claude:s"] || !got["claude:ssub"] {
		t.Errorf("hit sessions = %v, want s and its subagent", got)
	}
}

func TestStatsOwnerFilterAndSums(t *testing.T) {
	s, _ := openTestStore(t)
	seedOwners(t, s)
	loc := berlin(t)
	all, err := s.Stats(ctx, StatsFilter{Location: loc})
	if err != nil {
		t.Fatal(err)
	}
	solo, err := s.Stats(ctx, StatsFilter{Location: loc, Owners: &OwnerSet{[]Owner{ownSolo}}})
	if err != nil {
		t.Fatal(err)
	}
	if solo.Total != (Activity{Sessions: 2, Human: 1, Agent: 1}) {
		t.Errorf("solo = %+v", solo.Total)
	}
	if all.Total != (Activity{Sessions: 5, Human: 4, Agent: 3}) {
		t.Errorf("all = %+v", all.Total)
	}
	// the joined query of the token stats must be valid SQL
	if _, err := s.TokenStats(ctx, StatsFilter{Location: loc, Owners: &OwnerSet{[]Owner{ownSolo}}}); err != nil {
		t.Errorf("TokenStats: %v", err)
	}
}

func TestActivitySeriesAccountDimensions(t *testing.T) {
	s, _ := openTestStore(t)
	seedOwners(t, s)
	group := func(a, o string) string {
		if a == "acc-1" {
			return "firma"
		}
		return ""
	}
	tests := []struct {
		name string
		f    ActivityFilter
		want []string
	}{
		{"account splits organizations", ActivityFilter{Dimension: DimAccount},
			[]string{"acc-1/org-solo:2/1/1", "acc-1/org-team:1/1/1", "acc-2/org-priv:1/1/1", "unknown:1/1/0"}},
		{"group merges them", ActivityFilter{Dimension: DimAccountGroup, Group: group},
			[]string{"firma:3/2/2", "ungrouped:1/1/1", "unknown:1/1/0"}},
		{"no mapper means ungrouped", ActivityFilter{Dimension: DimAccountGroup},
			[]string{"ungrouped:4/3/3", "unknown:1/1/0"}},
		{"owners filter", ActivityFilter{Dimension: DimAccountGroup, Group: group, Owners: &OwnerSet{[]Owner{ownTeam, ownSolo}}},
			[]string{"firma:3/2/2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.f.Period, tt.f.Location = PeriodMonth, berlin(t)
			as, err := s.ActivitySeries(ctx, tt.f)
			if err != nil {
				t.Fatal(err)
			}
			if len(as.Buckets) != 1 {
				t.Fatalf("buckets = %d", len(as.Buckets))
			}
			got := cells(as.Buckets[0])
			if !sameSet(got, tt.want) {
				t.Errorf("cells = %v, want %v", got, tt.want)
			}
		})
	}
	if d, err := ParseDimension("account-group"); err != nil || d != DimAccountGroup || d.String() != "account-group" {
		t.Errorf("ParseDimension(account-group) = %v, %v", d, err)
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}

// TestOwnerInheritanceBoundaries pins what the inheritance does and does
// not do: a subagent takes its parent's login, one level up; a subagent of
// a parent without a login stays "unknown" (and is found by the empty
// owner, so nothing is lost from the sums); a subagent's own login wins; a
// subagent of a subagent has no login (the parent is no main session).
func TestOwnerInheritanceBoundaries(t *testing.T) {
	s, _ := openTestStore(t)
	sessions := []model.Session{
		{ID: "claude:m", Harness: "claude", NativeID: "m", Cwd: "/w", AccountID: "acc-1", OrganizationID: "org-1", StartedAt: ts("2026-10-01T10:00:00Z")},
		{ID: "claude:old", Harness: "claude", NativeID: "old", Cwd: "/w", StartedAt: ts("2026-10-01T11:00:00Z")},
		{ID: "claude:m-sub", Harness: "claude", NativeID: "m-sub", ParentID: "claude:m", Cwd: "/w", StartedAt: ts("2026-10-01T10:05:00Z")},
		{ID: "claude:old-sub", Harness: "claude", NativeID: "old-sub", ParentID: "claude:old", Cwd: "/w", StartedAt: ts("2026-10-01T11:05:00Z")},
		{ID: "claude:own-sub", Harness: "claude", NativeID: "own-sub", ParentID: "claude:m", Cwd: "/w", AccountID: "acc-2", OrganizationID: "org-2", StartedAt: ts("2026-10-01T10:06:00Z")},
		{ID: "claude:m-subsub", Harness: "claude", NativeID: "m-subsub", ParentID: "claude:m-sub", Cwd: "/w", StartedAt: ts("2026-10-01T10:07:00Z")},
		{ID: "claude:orphan", Harness: "claude", NativeID: "orphan", ParentID: "claude:gone", Cwd: "/w", StartedAt: ts("2026-10-01T12:00:00Z")},
	}
	var evs []model.Event
	for _, ss := range sessions {
		evs = append(evs, ev(ss.ID+"-1", ss.ID, "2026-10-01T10:30:00Z", model.ActorAgent, model.KindMessage, "assistant", "x"))
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/i.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
	want := map[string]Owner{
		"claude:m": {"acc-1", "org-1"}, "claude:m-sub": {"acc-1", "org-1"}, "claude:own-sub": {"acc-2", "org-2"},
		"claude:old": {}, "claude:old-sub": {}, "claude:m-subsub": {}, "claude:orphan": {},
	}
	for id, w := range want {
		si, err := s.Session(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got := si.Owner(); got != w {
			t.Errorf("%s: Owner = %v, want %v", id, got, w)
		}
	}
	// the filter, the series and Owners agree with Owner(): the sessions of
	// every owner add up to all sessions
	owners, err := s.Owners(ctx)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, o := range owners {
		list, err := s.Sessions(ctx, SessionFilter{Owners: &OwnerSet{[]Owner{o}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, si := range list {
			if si.Owner() != o {
				t.Errorf("%s listed for %v, Owner() is %v", si.ID, o, si.Owner())
			}
		}
		total += len(list)
	}
	if total != len(sessions) {
		t.Errorf("owners cover %d sessions, want %d", total, len(sessions))
	}
	as, err := s.ActivitySeries(ctx, ActivityFilter{Period: PeriodMonth, Dimension: DimAccount, Location: berlin(t)})
	if err != nil {
		t.Fatal(err)
	}
	if got := cells(as.Buckets[0]); !sameSet(got, []string{"acc-1/org-1:2/0/2", "acc-2/org-2:1/0/1", "unknown:4/0/4"}) {
		t.Errorf("cells = %v", got)
	}
}
