package worktime

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Ref is an issue or pull-request reference found in a session title or
// an own message: a bare number (#31), a pull request (PR #32) or a number
// in another repository (owner/repo#123), also from an issue or pull URL.
type Ref struct {
	Repo   string // owner/repo, "" for the project's own repository
	Number int
	PR     bool // written as PR #N, pull request #N or a /pull/ URL
}

// String writes the reference as it is read: #31, PR #32, owner/repo#123,
// PR owner/repo#124.
func (r Ref) String() string {
	s := r.Repo + "#" + strconv.Itoa(r.Number)
	if r.PR {
		return "PR " + s
	}
	return s
}

// less orders references: the own repository first, then by repository
// name, then by number.
func (r Ref) less(o Ref) bool {
	if r.Repo != o.Repo {
		return r.Repo < o.Repo
	}
	return r.Number < o.Number
}

var (
	// shortRef matches "#31", "PR #32", "owner/repo#123", "PR owner/repo#5".
	shortRef = regexp.MustCompile(`(?i)(?:\b(pr|mr|pull request|merge request)\s*)?([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)?#([0-9]+)\b`)
	// urlRef matches issue, pull and merge-request URLs of a forge.
	urlRef = regexp.MustCompile(`https?://[^\s/]+/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)/(?:-/)?(issues|pull|pulls|merge_requests)/([0-9]+)\b`)
)

// FindRefs returns the references of a text in order of first appearance,
// deduplicated by repository and number; a number that is written once as
// a pull request is a pull request. A "#" that follows a letter, digit or
// "&" is not a reference (C#1, &#39;, a URL fragment), nor is one followed
// by letters (#1a2b3c); a repository must not follow "/" or "." (a path).
func FindRefs(text string) []Ref {
	var out []Ref
	add := func(r Ref) {
		for i, o := range out {
			if o.Repo == r.Repo && o.Number == r.Number {
				out[i].PR = o.PR || r.PR
				return
			}
		}
		out = append(out, r)
	}
	for _, m := range urlRef.FindAllStringSubmatchIndex(text, -1) {
		if !validBefore(text, m[0], false) {
			continue
		}
		n, err := strconv.Atoi(text[m[6]:m[7]])
		if err != nil {
			continue
		}
		add(Ref{Repo: text[m[2]:m[3]], Number: n, PR: text[m[4]:m[5]] != "issues"})
	}
	for _, m := range shortRef.FindAllStringSubmatchIndex(text, -1) {
		pr, repo := m[2] >= 0, ""
		if m[4] >= 0 {
			repo = text[m[4]:m[5]]
		}
		// The character before the match must not be part of a word or
		// an entity; before a repository not part of a path either.
		if !validBefore(text, m[0], repo != "") {
			continue
		}
		if repo == "" && !pr && !validBefore(text, m[6]-1, false) {
			continue
		}
		if repo != "" && strings.Contains(text[max(0, m[0]-8):m[0]], "://") {
			continue
		}
		n, err := strconv.Atoi(text[m[6]:m[7]])
		if err != nil {
			continue
		}
		add(Ref{Repo: repo, Number: n, PR: pr})
	}
	return out
}

// validBefore reports whether the character before position i allows a
// reference to start at i: the start of the text or anything but a
// letter, digit, "_" or "&"; with path also not "/" or ".".
func validBefore(text string, i int, path bool) bool {
	if i <= 0 {
		return true
	}
	c := rune(text[i-1])
	if c >= 0x80 {
		return true
	}
	if path && (c == '/' || c == '.') {
		return false
	}
	return !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' && c != '&'
}

// SortRefs orders references in place: the own repository first, then by
// repository, then by number.
func SortRefs(refs []Ref) {
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].less(refs[j]) })
}

// MergeRefs returns the union of the lists, deduplicated by repository and
// number (a pull request wins) and sorted; nil when both are empty.
func MergeRefs(lists ...[]Ref) []Ref {
	var out []Ref
	for _, l := range lists {
		for _, r := range l {
			found := false
			for i, o := range out {
				if o.Repo == r.Repo && o.Number == r.Number {
					out[i].PR = o.PR || r.PR
					found = true
					break
				}
			}
			if !found {
				out = append(out, r)
			}
		}
	}
	SortRefs(out)
	return out
}

// Truncate shortens s to max runes, the last one "…"; max 0 means no
// limit.
func Truncate(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
