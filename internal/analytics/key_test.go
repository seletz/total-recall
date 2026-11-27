package analytics

import "testing"

func TestKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b []string
		same bool
	}{
		{"deterministic", []string{"model", "m1"}, []string{"model", "m1"}, true},
		{"dimension separates", []string{"model", "x"}, []string{"tool", "x"}, false},
		{"parts are not concatenated", []string{"harness", "ab", "c"}, []string{"harness", "a", "bc"}, false},
		{"empty natural key is a member", []string{"model", ""}, []string{"model"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ka, kb := Key(tt.a...), Key(tt.b...)
			if (ka == kb) != tt.same {
				t.Errorf("Key(%q)=%d, Key(%q)=%d, want same=%v", tt.a, ka, tt.b, kb, tt.same)
			}
			if ka == 0 {
				t.Errorf("Key(%q) = 0, reserved", tt.a)
			}
		})
	}
}

// TestKeyMatchesPostgres pins the value the PostgreSQL expression of the
// Key doc comment gives, so both targets produce the same keys:
// SELECT ('x' || substr(md5(concat_ws(chr(31), 'model', 'm1')), 1, 16))::bit(64)::bigint
func TestKeyMatchesPostgres(t *testing.T) {
	t.Parallel()
	const want = int64(-2923758452680610497) // computed by postgres:17-alpine
	if got := Key("model", "m1"); got != want {
		t.Errorf("Key(model, m1) = %d, want %d", got, want)
	}
}

func TestTilde(t *testing.T) {
	t.Parallel()
	tests := []struct{ path, home, want string }{
		{"/home/mara/src/kompass", "/home/mara", "~/src/kompass"},
		{"/home/mara", "/home/mara", "~"},
		{"/home/marabou/x", "/home/mara", "/home/marabou/x"},
		{"/srv/wetterwarte", "/home/mara", "/srv/wetterwarte"},
		{"/home/mara/x", "", "/home/mara/x"},
		{"", "/home/mara", ""},
	}
	for _, tt := range tests {
		got := Tilde(tt.path, tt.home)
		if got != tt.want {
			t.Errorf("Tilde(%q, %q) = %q, want %q", tt.path, tt.home, got, tt.want)
		}
		if tt.home != "" && got != "" && untilde(got, tt.home) != tt.path {
			t.Errorf("untilde(%q) = %q, want %q", got, untilde(got, tt.home), tt.path)
		}
	}
}
