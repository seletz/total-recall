package viewer

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestDiff(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 10, 2, 10, 30, 0, 0, time.UTC)
	tests := []struct {
		name string
		d    Diff
		want []string // lines of the plain rendering, in order
		not  []string
	}{
		{"change", Diff{Path: "a.md", Project: "kompass", Harness: "claude", From: t0, To: t1, FromNote: "version 1 of 2", ToNote: "version 2 of 2",
			Old: "one\ntwo\nthree\n", New: "one\n2\nthree\n"},
			[]string{"a.md", "kompass  claude", "2026-10-01 09:00 version 1 of 2 → 2026-10-02 10:30 version 2 of 2", "+1 -1", "@@ -1,3 +1,3 @@", "   one", " - two", " + 2", "   three"}, nil},
		{"no earlier version", Diff{Path: "a.md", To: t1, New: "x\ny\n"},
			[]string{"(no earlier version) → 2026-10-02 10:30", "+2 -0", "@@ -0,0 +1,2 @@", " + x", " + y"}, []string{" - "}},
		{"identical", Diff{Path: "a.md", From: t0, To: t1, Old: "x\n", New: "x\n"},
			[]string{"+0 -0", "no differences"}, []string{"@@"}},
		{"blank line and tab", Diff{Path: "a.md", From: t0, To: t1, Old: "a\n\n", New: "a\n\n\tb\n"},
			[]string{" +     b"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plain(tt.d.Render(env, 80))
			pos := 0
			for _, want := range tt.want {
				i := strings.Index(got[pos:], want)
				if i < 0 {
					t.Fatalf("rendering lacks %q after offset %d:\n%s", want, pos, got)
				}
				pos += i + len(want)
			}
			for _, not := range tt.not {
				if strings.Contains(got, not) {
					t.Errorf("rendering has %q:\n%s", not, got)
				}
			}
		})
	}
}

// TestDiffWraps: a long line wraps within the width, the continuation
// lines are indented behind the prefix.
func TestDiffWraps(t *testing.T) {
	long := strings.Repeat("word ", 30)
	c := Diff{Path: "a.md", To: time.Now(), New: long + "\n"}.Render(env, 40)
	if c.Anchor != -1 {
		t.Errorf("anchor = %d, want none", c.Anchor)
	}
	var plus int
	for _, l := range c.Body[1:] {
		s := ansi.Strip(l)
		if len(s) > 40 {
			t.Errorf("line %q is wider than 40", s)
		}
		if strings.HasPrefix(s, " + ") {
			plus++
		}
	}
	if plus != 1 || len(c.Body) < 4 {
		t.Errorf("%d marked lines in %d body lines, want one marked line that wraps", plus, len(c.Body))
	}
}
