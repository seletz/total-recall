package session

import (
	"sort"
	"strings"
	"unicode"
)

// Match is one highlighted range: byte offsets Start (inclusive) and End
// (exclusive) within lines[Line].
type Match struct {
	Line, Start, End int
}

// Matches finds every occurrence of terms in lines, ordered by line and
// start, with overlapping or touching ranges merged. Matching is a
// case-insensitive substring match with diacritics removed on both sides,
// the pragmatic counterpart of the FTS5 tokenizer `unicode61
// remove_diacritics 2` the archive searches with: "uber" finds "Über", but
// "ß" stays "ß".
func Matches(lines []string, terms []string) []Match {
	var folded [][]rune
	for _, t := range terms {
		if r, _ := fold(t); len(r) > 0 {
			folded = append(folded, r)
		}
	}
	if len(folded) == 0 {
		return nil
	}
	var out []Match
	for i, line := range lines {
		runes, offsets := fold(line)
		var found []Match
		for _, term := range folded {
			for start := 0; start+len(term) <= len(runes); {
				j := indexRunes(runes[start:], term)
				if j < 0 {
					break
				}
				found = append(found, Match{i, offsets[start+j], offsets[start+j+len(term)]})
				start += j + 1
			}
		}
		out = append(out, merge(found)...)
	}
	return out
}

// merge sorts matches of one line and joins ranges that overlap or touch.
func merge(ms []Match) []Match {
	sort.Slice(ms, func(a, b int) bool {
		if ms[a].Start != ms[b].Start {
			return ms[a].Start < ms[b].Start
		}
		return ms[a].End > ms[b].End
	})
	var out []Match
	for _, m := range ms {
		if n := len(out); n > 0 && m.Start <= out[n-1].End {
			out[n-1].End = max(out[n-1].End, m.End)
			continue
		}
		out = append(out, m)
	}
	return out
}

func indexRunes(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalRunes(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func equalRunes(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fold lower-cases s and strips diacritics: precomposed Latin letters are
// replaced by their base letter, combining marks are dropped. It returns the
// folded runes and, for every folded rune plus one past the end, the byte
// offset in s where it starts, so matches map back to the original string.
func fold(s string) ([]rune, []int) {
	runes := make([]rune, 0, len(s))
	offsets := make([]int, 0, len(s)+1)
	for i, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if base, ok := baseLetter[r]; ok {
			r = base
		}
		runes = append(runes, unicode.ToLower(r))
		offsets = append(offsets, i)
	}
	return runes, append(offsets, len(s))
}

// baseLetter maps precomposed Latin letters with diacritics to their base
// letter (Latin-1 Supplement and Latin Extended-A, the letters of the
// European languages the archive is likely to contain).
var baseLetter = func() map[rune]rune {
	m := map[rune]rune{}
	add := func(base rune, variants string) {
		for _, v := range variants {
			m[v] = base
		}
	}
	add('A', "ÀÁÂÃÄÅĀĂĄ")
	add('a', "àáâãäåāăą")
	add('C', "ÇĆĈĊČ")
	add('c', "çćĉċč")
	add('D', "ĎĐ")
	add('d', "ďđ")
	add('E', "ÈÉÊËĒĔĖĘĚ")
	add('e', "èéêëēĕėęě")
	add('G', "ĜĞĠĢ")
	add('g', "ĝğġģ")
	add('H', "ĤĦ")
	add('h', "ĥħ")
	add('I', "ÌÍÎÏĨĪĬĮİ")
	add('i', "ìíîïĩīĭįı")
	add('J', "Ĵ")
	add('j', "ĵ")
	add('K', "Ķ")
	add('k', "ķ")
	add('L', "ĹĻĽĿŁ")
	add('l', "ĺļľŀł")
	add('N', "ÑŃŅŇ")
	add('n', "ñńņň")
	add('O', "ÒÓÔÕÖØŌŎŐ")
	add('o', "òóôõöøōŏő")
	add('R', "ŔŖŘ")
	add('r', "ŕŗř")
	add('S', "ŚŜŞŠ")
	add('s', "śŝşš")
	add('T', "ŢŤŦ")
	add('t', "ţťŧ")
	add('U', "ÙÚÛÜŨŪŬŮŰŲ")
	add('u', "ùúûüũūŭůűų")
	add('W', "Ŵ")
	add('w', "ŵ")
	add('Y', "ÝŶŸ")
	add('y', "ýÿŷ")
	add('Z', "ŹŻŽ")
	add('z', "źżž")
	return m
}()

// HighlightLine renders one plain line with its matches ms (byte ranges
// within line) in st.Match, the match at current (an index into ms, or -1)
// in st.Selected and the text between them in st.Base. Every segment is
// styled on its own: a style wrapped around styled text would end at the
// inner reset.
func HighlightLine(line string, ms []Match, current int, st LineStyles) string {
	base := func(s string) string {
		if s == "" {
			return ""
		}
		return st.Base.Render(s)
	}
	if len(ms) == 0 {
		return base(line)
	}
	var b strings.Builder
	pos := 0
	for i, m := range ms {
		b.WriteString(base(line[pos:m.Start]))
		if i == current {
			b.WriteString(st.Selected.Render(line[m.Start:m.End]))
		} else {
			b.WriteString(st.Match.Render(line[m.Start:m.End]))
		}
		pos = m.End
	}
	b.WriteString(base(line[pos:]))
	return b.String()
}
