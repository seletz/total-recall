package linediff

import (
	"reflect"
	"strings"
	"testing"
)

func render(ops []Op) string {
	var b strings.Builder
	for _, o := range ops {
		b.WriteString(string(o.Kind) + o.Text + "|")
	}
	return b.String()
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{"equal", "a\nb\n", "a\nb\n", " a| b|"},
		{"both empty", "", "", ""},
		{"all new", "", "a\nb\n", "+a|+b|"},
		{"all gone", "a\nb\n", "", "-a|-b|"},
		{"change in the middle", "a\nb\nc\n", "a\nX\nc\n", " a|-b|+X| c|"},
		{"insert", "a\nc\n", "a\nb\nc\n", " a|+b| c|"},
		{"delete", "a\nb\nc\n", "a\nc\n", " a|-b| c|"},
		{"two changes", "1\n2\n3\n4\n5\n", "1\nB\n3\nD\n5\n", " 1|-2|+B| 3|-4|+D| 5|"},
		{"moved line", "a\nb\nc\n", "b\nc\na\n", "-a| b| c|+a|"},
		{"trailing newline ignored", "a\nb", "a\nb\n", " a| b|"},
		{"repeated lines", "x\nx\nx\n", "x\nx\n", " x| x|-x|"},
		{"crlf", "a\r\nb\r\n", "a\nc\n", " a|-b|+c|"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := render(Diff(SplitLines(tt.a), SplitLines(tt.b))); got != tt.want {
				t.Errorf("Diff = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestLineNumbers(t *testing.T) {
	ops := Diff(SplitLines("a\nb\nc\n"), SplitLines("a\nX\nY\nc\n"))
	var got [][2]int
	for _, o := range ops {
		got = append(got, [2]int{o.Old, o.New})
	}
	want := [][2]int{{1, 1}, {2, 0}, {0, 2}, {0, 3}, {3, 4}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("line numbers = %v, want %v", got, want)
	}
}

func TestCount(t *testing.T) {
	add, del := Count(Diff(SplitLines("a\nb\nc\n"), SplitLines("a\nX\nY\nc\n")))
	if add != 2 || del != 1 {
		t.Errorf("Count = +%d -%d, want +2 -1", add, del)
	}
}

// TestLargeFallback: inputs beyond the table size still give a correct
// (if not minimal) edit script.
func TestLargeFallback(t *testing.T) {
	var a, b []string
	for i := 0; i < 3000; i++ {
		a = append(a, "a"+string(rune('0'+i%10)))
		b = append(b, "b"+string(rune('0'+i%10)))
	}
	ops := Diff(a, b)
	var oldN, newN int
	for _, o := range ops {
		switch o.Kind {
		case Delete:
			oldN++
		case Insert:
			newN++
		}
	}
	if oldN != len(a) || newN != len(b) {
		t.Errorf("script has %d deletions and %d insertions, want %d and %d", oldN, newN, len(a), len(b))
	}
}

func TestHunks(t *testing.T) {
	var a, b []string
	for i := 1; i <= 30; i++ {
		l := "line " + string(rune('A'+i%26))
		a, b = append(a, l+string(rune('0'+i/10))+string(rune('0'+i%10))), append(b, l+string(rune('0'+i/10))+string(rune('0'+i%10)))
	}
	b[2] = "changed 3"
	b[25] = "changed 26"
	hs := Hunks(Diff(a, b), 2)
	if len(hs) != 2 {
		t.Fatalf("%d hunks, want 2: %+v", len(hs), hs)
	}
	if h := hs[0]; h.OldStart != 1 || h.OldLines != 5 || h.NewStart != 1 || h.NewLines != 5 {
		t.Errorf("first hunk = %+v, want -1,5 +1,5", h)
	}
	if h := hs[1]; h.OldStart != 24 || h.OldLines != 5 {
		t.Errorf("second hunk = %+v, want -24,5", h)
	}
	if got := Hunks(Diff(a, a), 2); len(got) != 0 {
		t.Errorf("identical inputs give %d hunks", len(got))
	}
	// Changes close together share a hunk.
	b2 := append([]string(nil), a...)
	b2[2], b2[5] = "x", "y"
	if got := Hunks(Diff(a, b2), 2); len(got) != 1 {
		t.Errorf("close changes give %d hunks, want 1", len(got))
	}
	// Pure insertion into an empty side numbers from 0.
	if h := Hunks(Diff(nil, []string{"a"}), 3)[0]; h.OldStart != 0 || h.OldLines != 0 || h.NewStart != 1 || h.NewLines != 1 {
		t.Errorf("insertion hunk = %+v, want -0,0 +1,1", h)
	}
}
