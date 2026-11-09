// Package linediff computes a line diff of two texts: the edit script that
// turns one list of lines into the other and its grouping into hunks with
// context, as `diff -u` prints them. It is the diff of the memory view of
// the TUI, where files are small (kilobytes), so a quadratic table is fine;
// above maxCells the script is the plain replacement of the changed middle.
package linediff

import "strings"

// Kind says what an Op does to a line.
type Kind byte

// Kinds of an Op; the value is the diff prefix of the line.
const (
	Equal  Kind = ' '
	Delete Kind = '-'
	Insert Kind = '+'
)

// Op is one line of the edit script.
type Op struct {
	Kind Kind
	Text string
	// Old and New are the 1-based line numbers in the old and the new text,
	// 0 on the side where the line does not exist.
	Old, New int
}

// maxCells is the largest table of the longest common subsequence; a
// bigger middle part is replaced as a whole.
const maxCells = 4_000_000

// SplitLines splits text into lines: "\n" or "\r\n" ends a line, a final
// line end does not start another one; "" has no lines.
func SplitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// Diff returns the edit script from a to b: every line of a and of b
// exactly once, in order. Equal lines are kept as far as a longest common
// subsequence of the lines finds them.
func Diff(a, b []string) []Op {
	// Common head and tail need no table.
	head := 0
	for head < len(a) && head < len(b) && a[head] == b[head] {
		head++
	}
	tail := 0
	for tail < len(a)-head && tail < len(b)-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}
	ma, mb := a[head:len(a)-tail], b[head:len(b)-tail]
	var ops []Op
	for i := 0; i < head; i++ {
		ops = append(ops, Op{Kind: Equal, Text: a[i]})
	}
	ops = append(ops, middle(ma, mb)...)
	for i := len(a) - tail; i < len(a); i++ {
		ops = append(ops, Op{Kind: Equal, Text: a[i]})
	}
	// Number the lines.
	o, n := 0, 0
	for i := range ops {
		switch ops[i].Kind {
		case Equal:
			o, n = o+1, n+1
			ops[i].Old, ops[i].New = o, n
		case Delete:
			o++
			ops[i].Old = o
		case Insert:
			n++
			ops[i].New = n
		}
	}
	return ops
}

// middle diffs the part of two texts between their common head and tail.
func middle(a, b []string) []Op {
	switch {
	case len(a) == 0 && len(b) == 0:
		return nil
	case len(a) == 0 || len(b) == 0 || len(a)*len(b) > maxCells:
		return replace(a, b)
	}
	// lcs[i][j] is the length of the longest common subsequence of
	// a[i:] and b[j:].
	lcs := make([][]int32, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var ops []Op
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, Op{Kind: Equal, Text: a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, Op{Kind: Delete, Text: a[i]})
			i++
		default:
			ops = append(ops, Op{Kind: Insert, Text: b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, Op{Kind: Delete, Text: a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, Op{Kind: Insert, Text: b[j]})
	}
	return ops
}

// replace deletes all of a, then inserts all of b.
func replace(a, b []string) []Op {
	ops := make([]Op, 0, len(a)+len(b))
	for _, l := range a {
		ops = append(ops, Op{Kind: Delete, Text: l})
	}
	for _, l := range b {
		ops = append(ops, Op{Kind: Insert, Text: l})
	}
	return ops
}

// Count returns the number of inserted and deleted lines of a script.
func Count(ops []Op) (added, deleted int) {
	for _, o := range ops {
		switch o.Kind {
		case Insert:
			added++
		case Delete:
			deleted++
		}
	}
	return added, deleted
}

// Hunk is a run of changes with the equal lines around it, as `diff -u`
// groups them. Starts are 1-based; a side without lines starts at 0.
type Hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
	Ops                []Op
}

// Hunks groups a script into hunks with context equal lines around every
// change; changes closer than twice the context share a hunk. An
// unchanged text has none.
func Hunks(ops []Op, context int) []Hunk {
	var hunks []Hunk
	i := 0
	for i < len(ops) {
		// Find the next change.
		for i < len(ops) && ops[i].Kind == Equal {
			i++
		}
		if i == len(ops) {
			break
		}
		start := max(i-context, 0)
		// Extend to the end of the change run and its neighbours.
		end, lastChange := i, i
		for end < len(ops) {
			if ops[end].Kind != Equal {
				lastChange = end
			} else if end-lastChange > 2*context {
				break
			}
			end++
		}
		end = min(lastChange+context+1, len(ops))
		hunks = append(hunks, hunkOf(ops[start:end]))
		i = end
	}
	return hunks
}

// hunkOf computes the header numbers of a run of ops.
func hunkOf(ops []Op) Hunk {
	h := Hunk{Ops: ops}
	for _, o := range ops {
		if o.Old > 0 {
			if h.OldStart == 0 {
				h.OldStart = o.Old
			}
			h.OldLines++
		}
		if o.New > 0 {
			if h.NewStart == 0 {
				h.NewStart = o.New
			}
			h.NewLines++
		}
	}
	return h
}
