package store

import (
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

// TestMergeSessionRelations: the continuation link and the account are
// "last non-empty value wins" fields, like the titles.
func TestMergeSessionRelations(t *testing.T) {
	tests := []struct {
		name     string
		old, new model.Session
		want     model.Session
	}{
		{"fills empty",
			model.Session{ID: "c:1"},
			model.Session{ID: "c:1", ContinuedIn: "c:2", AccountID: "acc-1", OrganizationID: "org-1"},
			model.Session{ID: "c:1", ContinuedIn: "c:2", AccountID: "acc-1", OrganizationID: "org-1"}},
		{"empty never overwrites",
			model.Session{ID: "c:1", ContinuedIn: "c:2", AccountID: "acc-1", OrganizationID: "org-1"},
			model.Session{ID: "c:1", Name: "n"},
			model.Session{ID: "c:1", Name: "n", ContinuedIn: "c:2", AccountID: "acc-1", OrganizationID: "org-1"}},
		{"last write wins",
			model.Session{ID: "c:1", ContinuedIn: "c:2", AccountID: "acc-1", OrganizationID: "org-1"},
			model.Session{ID: "c:1", ContinuedIn: "c:3", AccountID: "acc-2", OrganizationID: "org-2"},
			model.Session{ID: "c:1", ContinuedIn: "c:3", AccountID: "acc-2", OrganizationID: "org-2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeSession(tt.old, tt.new); got != tt.want {
				t.Errorf("mergeSession:\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// seedRelations writes a chain a → b → c (a continued in b, b in c), with
// c not ingested, and accounts: a and b on acc-1, a subagent of b without
// an account of its own, d on acc-2 and e without any account.
func seedRelations(t *testing.T, s *Store) {
	t.Helper()
	sessions := []model.Session{
		{ID: "claude:a", Harness: "claude", NativeID: "a", Name: "A", Cwd: "/w", ContinuedIn: "claude:b",
			AccountID: "acc-1", OrganizationID: "org-1",
			StartedAt: ts("2026-10-01T10:00:00Z"), EndedAt: ts("2026-10-01T11:00:00Z")},
		{ID: "claude:b", Harness: "claude", NativeID: "b", Name: "B", Cwd: "/w", ContinuedIn: "claude:c",
			AccountID: "acc-1", OrganizationID: "org-1",
			StartedAt: ts("2026-10-02T10:00:00Z"), EndedAt: ts("2026-10-02T11:00:00Z")},
		{ID: "claude:bsub", Harness: "claude", NativeID: "bsub", ParentID: "claude:b", Cwd: "/w",
			StartedAt: ts("2026-10-02T10:05:00Z"), EndedAt: ts("2026-10-02T10:06:00Z")},
		{ID: "claude:d", Harness: "claude", NativeID: "d", Name: "D", Cwd: "/w", AccountID: "acc-2", OrganizationID: "org-2",
			StartedAt: ts("2026-10-03T10:00:00Z"), EndedAt: ts("2026-10-03T11:00:00Z")},
		{ID: "claude:e", Harness: "claude", NativeID: "e", Name: "E", Cwd: "/w",
			StartedAt: ts("2026-10-04T10:00:00Z"), EndedAt: ts("2026-10-04T11:00:00Z")},
	}
	evs := []model.Event{
		ev("claude:a-1", "claude:a", "2026-10-01T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "a"),
		ev("claude:b-1", "claude:b", "2026-10-02T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "b"),
		ev("claude:bs-1", "claude:bsub", "2026-10-02T10:05:00Z", model.ActorAgent, model.KindMessage, "assistant", "bs"),
		ev("claude:d-1", "claude:d", "2026-10-03T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "d"),
		ev("claude:d-2", "claude:d", "2026-10-03T10:01:00Z", model.ActorAgent, model.KindMessage, "assistant", "d2"),
		ev("claude:e-1", "claude:e", "2026-10-04T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "e"),
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/a.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRelationsRoundTrip(t *testing.T) {
	s, _ := openTestStore(t)
	seedRelations(t, s)
	tests := []struct {
		id                                  string
		continuedIn, continuedFrom, account string
		organization                        string
	}{
		{"claude:a", "claude:b", "", "acc-1", "org-1"},
		{"claude:b", "claude:c", "claude:a", "acc-1", "org-1"},
		{"claude:bsub", "", "", "", ""},
		{"claude:d", "", "", "acc-2", "org-2"},
		{"claude:e", "", "", "", ""},
	}
	for _, tt := range tests {
		si, err := s.Session(ctx, tt.id)
		if err != nil {
			t.Fatalf("Session(%s): %v", tt.id, err)
		}
		if si.ContinuedIn != tt.continuedIn || si.ContinuedFrom != tt.continuedFrom ||
			si.AccountID != tt.account || si.OrganizationID != tt.organization {
			t.Errorf("%s: continued_in %q from %q account %q org %q, want %q %q %q %q", tt.id,
				si.ContinuedIn, si.ContinuedFrom, si.AccountID, si.OrganizationID,
				tt.continuedIn, tt.continuedFrom, tt.account, tt.organization)
		}
	}
	// The link survives even though claude:c was never ingested, and
	// FindSession resolves the target's absence as not found.
	if _, err := s.FindSession(ctx, "claude:c"); err != ErrNotFound {
		t.Errorf("FindSession(c) = %v, want ErrNotFound", err)
	}
}

func TestSessionsAccountFilter(t *testing.T) {
	s, _ := openTestStore(t)
	seedRelations(t, s)
	tests := []struct {
		name string
		f    SessionFilter
		want string
	}{
		{"exact", SessionFilter{Account: "acc-1"}, "claude:b,claude:a"},
		{"prefix", SessionFilter{Account: "acc-2"}, "claude:d"},
		{"prefix of several", SessionFilter{Account: "acc"}, "claude:d,claude:b,claude:a"},
		{"unknown", SessionFilter{Account: "nope"}, ""},
		{"escapes like", SessionFilter{Account: "acc_"}, ""},
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

func TestActivitySeriesByAccount(t *testing.T) {
	s, _ := openTestStore(t)
	seedRelations(t, s)
	as, err := s.ActivitySeries(ctx, ActivityFilter{Period: PeriodMonth, Dimension: DimAccount, Location: berlin(t)})
	if err != nil {
		t.Fatal(err)
	}
	// acc-1: a, b and the subagent of b (3 events), acc-2: d (2 events),
	// e has no account and counts under UnknownAccountKey.
	if want := []string{"acc-1", "acc-2", UnknownAccountKey}; !equalStrings(as.Keys, want) {
		t.Errorf("keys = %v, want %v", as.Keys, want)
	}
	if len(as.Buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(as.Buckets))
	}
	if got, want := cells(as.Buckets[0]), []string{"acc-1:3/2/1", "acc-2:1/1/1", "unknown:1/1/0"}; !equalStrings(got, want) {
		t.Errorf("cells = %v, want %v", got, want)
	}
	if len(as.Dirs) != 0 {
		t.Errorf("dirs = %v, want none", as.Dirs)
	}
	if d, err := ParseDimension("account"); err != nil || d != DimAccount || d.String() != "account" {
		t.Errorf("ParseDimension(account) = %v, %v", d, err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
