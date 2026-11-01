package stats

import "testing"

var ghostty = Env{Term: "xterm-ghostty", TermProgram: "ghostty", TermProgramVersion: "1.4.0"}

// TestDecide is the decision table of the chart mode: flag × probe × environment.
func TestDecide(t *testing.T) {
	tests := []struct {
		name  string
		mode  Graphics
		probe Probe
		env   Env
		want  Decision
	}{
		// --graphics text never shows an image, whatever the terminal says.
		{"text ignores probe", GraphicsText, ProbeSupported, ghostty, Decision{}},
		{"text ignores pending", GraphicsText, ProbePending, ghostty, Decision{}},
		// --graphics kitty forces the image without a probe.
		{"kitty forces", GraphicsKitty, ProbeNotDetected, Env{}, Decision{Image: true, Reason: "forced"}},
		{"kitty forces in tmux", GraphicsKitty, ProbeTimeout, Env{Tmux: true}, Decision{Image: true, Reason: "forced"}},
		// auto: the probe decides first.
		{"auto pending", GraphicsAuto, ProbePending, ghostty, Decision{Reason: "probing…"}},
		{"auto not detected", GraphicsAuto, ProbeNotDetected, Env{Term: "xterm-256color", TermProgram: "Apple_Terminal"}, Decision{Reason: "not detected"}},
		{"auto timeout", GraphicsAuto, ProbeTimeout, ghostty, Decision{Reason: "no answer"}},
		{"auto timeout in tmux hints at passthrough", GraphicsAuto, ProbeTimeout, Env{Tmux: true, TermProgram: "tmux"}, Decision{Reason: "no answer; tmux allow-passthrough?"}},
		{"auto skipped", GraphicsAuto, ProbeSkipped, ghostty, Decision{Reason: "not probed"}},
		// auto, probe positive: only terminals with Unicode placeholders.
		{"auto ghostty", GraphicsAuto, ProbeSupported, ghostty, Decision{Image: true}},
		{"auto ghostty by TERM_PROGRAM only", GraphicsAuto, ProbeSupported, Env{Term: "xterm-256color", TermProgram: "ghostty"}, Decision{Image: true}},
		{"auto kitty", GraphicsAuto, ProbeSupported, Env{Term: "xterm-kitty"}, Decision{Image: true}},
		{"auto wezterm", GraphicsAuto, ProbeSupported, Env{Term: "xterm-256color", TermProgram: "WezTerm"}, Decision{Reason: "no placeholders in WezTerm"}},
		{"auto unknown terminal", GraphicsAuto, ProbeSupported, Env{Term: "xterm-256color"}, Decision{Reason: "no placeholders in xterm-256color"}},
		{"auto nameless terminal", GraphicsAuto, ProbeSupported, Env{}, Decision{Reason: "no placeholders in this terminal"}},
		// tmux hides the outer terminal; the Ghostty hint survives in the server's environment.
		{"auto tmux with ghostty hint", GraphicsAuto, ProbeSupported, Env{Term: "tmux-256color", TermProgram: "tmux", Tmux: true, Ghostty: true}, Decision{Image: true}},
		{"auto tmux without hint", GraphicsAuto, ProbeSupported, Env{Term: "tmux-256color", TermProgram: "tmux", Tmux: true}, Decision{Reason: "outer terminal unknown"}},
		{"auto tmux old ghostty", GraphicsAuto, ProbeSupported, Env{TermProgram: "ghostty", TermProgramVersion: "1.3.1", Tmux: true}, Decision{Reason: "ghostty 1.3.1 in tmux needs 1.4"}},
		{"auto tmux new ghostty", GraphicsAuto, ProbeSupported, Env{TermProgram: "ghostty", TermProgramVersion: "1.4.0", Tmux: true}, Decision{Image: true}},
		{"auto tmux ghostty unknown version", GraphicsAuto, ProbeSupported, Env{TermProgram: "ghostty", Tmux: true}, Decision{Image: true}},
		{"old ghostty outside tmux is fine", GraphicsAuto, ProbeSupported, Env{TermProgram: "ghostty", TermProgramVersion: "1.2.0"}, Decision{Image: true}},
		// An empty mode behaves like auto.
		{"empty is auto", "", ProbeSupported, ghostty, Decision{Image: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Decide(tt.mode, tt.probe, tt.env); got != tt.want {
				t.Errorf("Decide(%q, %v, %+v) = %+v, want %+v", tt.mode, tt.probe, tt.env, got, tt.want)
			}
		})
	}
}

func TestDecisionLabel(t *testing.T) {
	tests := []struct {
		d     Decision
		shown bool
		want  string
	}{
		{Decision{}, false, "text chart"},
		{Decision{Reason: "not detected"}, false, "text chart (kitty: not detected)"},
		{Decision{Image: true}, true, "image chart (kitty)"},
		{Decision{Image: true, Reason: "forced"}, true, "image chart (kitty, forced)"},
		{Decision{Image: true}, false, "text chart (kitty: rendering…)"},
		{Decision{Image: true, Reason: "forced"}, false, "text chart (kitty: rendering…)"},
	}
	for _, tt := range tests {
		if got := tt.d.Label(tt.shown); got != tt.want {
			t.Errorf("%+v.Label(%v) = %q, want %q", tt.d, tt.shown, got, tt.want)
		}
	}
}

func TestNextGraphics(t *testing.T) {
	tests := []struct{ in, want Graphics }{
		{GraphicsAuto, GraphicsText},
		{GraphicsText, GraphicsKitty},
		{GraphicsKitty, GraphicsAuto},
		{"", GraphicsText},
	}
	for _, tt := range tests {
		if got := tt.in.Next(); got != tt.want {
			t.Errorf("%q.Next() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestVersionBefore(t *testing.T) {
	tests := []struct {
		v    string
		want bool // before 1.4.0
	}{
		{"1.3.1", true},
		{"1.4.0", false},
		{"1.4.1", false},
		{"1.10.0", false},
		{"2.0", false},
		{"0.9", true},
		{"1.4.0-main+abc", false},
		{"1.3.9-dev", true},
		{"", false},
		{"garbage", false},
	}
	for _, tt := range tests {
		if got := versionBefore(tt.v, 1, 4); got != tt.want {
			t.Errorf("versionBefore(%q, 1.4) = %v, want %v", tt.v, got, tt.want)
		}
	}
}

func TestEnvFrom(t *testing.T) {
	vars := map[string]string{
		"TERM": "tmux-256color", "TERM_PROGRAM": "tmux", "TERM_PROGRAM_VERSION": "3.8",
		"TMUX": "/tmp/tmux-501/default,1,0", "GHOSTTY_RESOURCES_DIR": "/Applications/Ghostty.app/Contents/Resources/ghostty",
	}
	got := EnvFrom(func(k string) string { return vars[k] })
	want := Env{Term: "tmux-256color", TermProgram: "tmux", TermProgramVersion: "3.8", Tmux: true, Ghostty: true}
	if got != want {
		t.Errorf("EnvFrom = %+v, want %+v", got, want)
	}
	if got := EnvFrom(func(string) string { return "" }); got != (Env{}) {
		t.Errorf("EnvFrom(empty) = %+v, want zero", got)
	}
}
