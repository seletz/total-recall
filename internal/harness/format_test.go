package harness

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"2.1.10", "2.1.9", 1},
		{"2.1.9", "2.1.10", -1},
		{"2.1.316", "2.1.316", 0},
		{"2.1", "2.1.0", -1},
		{"3.0.0", "2.99.99", 1},
		{"2.1.0-beta", "2.1.0-alpha", 1},
	}
	for _, tt := range tests {
		if got := CompareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestFormatReportClean(t *testing.T) {
	r := NewFormatReport("x")
	if !r.Clean() {
		t.Error("empty report not clean")
	}
	r.SetVersions(map[string]int64{"1.2": 1, "1.10": 2})
	if !r.Clean() || r.Versions[0].Version != "1.10" {
		t.Errorf("versions = %v", r.Versions)
	}
	r.Malformed.Add("f:1")
	r.Malformed.Add("f:2")
	if r.Clean() || r.Malformed.Count != 2 || r.Malformed.Example != "f:1" {
		t.Errorf("malformed = %+v", r.Malformed)
	}
}
