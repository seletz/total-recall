package worktime

import (
	"reflect"
	"testing"
)

func TestFindRefs(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"empty", "", nil},
		{"bare", "fix #31 please", []string{"#31"}},
		{"parenthesised", "Umlaute (#48) and [#49], #50.", []string{"#48", "#49", "#50"}},
		{"start of text", "#7 is open", []string{"#7"}},
		{"pr", "review PR #32", []string{"PR #32"}},
		{"pr lowercase glued", "pr#32 and PR#33", []string{"PR #32", "PR #33"}},
		{"pull request words", "the pull request #40 is green", []string{"PR #40"}},
		{"repo", "see owner/repo#123", []string{"owner/repo#123"}},
		{"repo pr", "PR owner/repo#5", []string{"PR owner/repo#5"}},
		{"repo with dots and dashes", "mara/kompass-web#9 and a.b/c_d#10", []string{"mara/kompass-web#9", "a.b/c_d#10"}},
		{"issue url", "https://github.com/owner/repo/issues/123 again", []string{"owner/repo#123"}},
		{"pull url", "see https://github.com/owner/repo/pull/124.", []string{"PR owner/repo#124"}},
		{"gitlab mr url", "https://gitlab.invalid/owner/repo/-/merge_requests/8", []string{"PR owner/repo#8"}},
		{"slash separated", "#19/#26", []string{"#19", "#26"}},
		{"dedup keeps order", "#3, #2, #3 and #2", []string{"#3", "#2"}},
		{"pr form wins", "#32 then PR #32", []string{"PR #32"}},
		{"issue then pr same number", "PR #32 then #32", []string{"PR #32"}},
		{"word before hash", "C#1 foo#2 x_y#3", nil},
		{"html entity", "&#39;quoted&#39;", nil},
		{"shebang and headings", "#!/bin/sh\n# 1 heading\n## 2", nil},
		{"hex colour", "color #1a2b3c and #fff", nil},
		{"digits then letters", "#12abc", nil},
		{"two part path reads as a repository", "src/foo#1", []string{"src/foo#1"}},
		{"longer path", "internal/cli/foo.go#12 and ./a/b#3", nil},
		{"url fragment", "https://example.test/docs#3", nil},
		{"number alone", "issue 31 and PR 32", nil},
		{"multiline", "first #1\nsecond PR #2\nthird owner/repo#3", []string{"#1", "PR #2", "owner/repo#3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, r := range FindRefs(tt.text) {
				got = append(got, r.String())
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindRefs(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestRefString(t *testing.T) {
	tests := []struct {
		ref  Ref
		want string
	}{
		{Ref{Number: 31}, "#31"},
		{Ref{Number: 32, PR: true}, "PR #32"},
		{Ref{Repo: "owner/repo", Number: 123}, "owner/repo#123"},
		{Ref{Repo: "owner/repo", Number: 124, PR: true}, "PR owner/repo#124"},
	}
	for _, tt := range tests {
		if got := tt.ref.String(); got != tt.want {
			t.Errorf("%+v = %q, want %q", tt.ref, got, tt.want)
		}
	}
}

func TestSortRefs(t *testing.T) {
	in := []Ref{{Repo: "b/b", Number: 1}, {Number: 20}, {Repo: "a/a", Number: 9}, {Number: 3, PR: true}, {Repo: "a/a", Number: 2}}
	SortRefs(in)
	want := []string{"PR #3", "#20", "a/a#2", "a/a#9", "b/b#1"}
	var got []string
	for _, r := range in {
		got = append(got, r.String())
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SortRefs = %v, want %v", got, want)
	}
}

func TestMergeRefs(t *testing.T) {
	a := []Ref{{Number: 3}, {Number: 1}}
	b := []Ref{{Number: 3, PR: true}, {Number: 2}}
	got := MergeRefs(a, b)
	want := []Ref{{Number: 1}, {Number: 2}, {Number: 3, PR: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeRefs = %+v, want %+v", got, want)
	}
	if got := MergeRefs(nil, nil); got != nil {
		t.Errorf("MergeRefs(nil, nil) = %+v, want nil", got)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		max  int
		want string
	}{
		{"", 5, ""},
		{"abc", 0, "abc"},
		{"abc", 3, "abc"},
		{"abcd", 3, "ab…"},
		{"äöüß", 3, "äö…"},
		{"abcd", 1, "…"},
	}
	for _, tt := range tests {
		if got := Truncate(tt.s, tt.max); got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
		}
	}
}
