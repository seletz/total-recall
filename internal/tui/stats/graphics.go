package stats

import (
	"os"
	"strconv"
	"strings"
)

// Graphics selects how the chart is drawn: as text, as a Kitty Graphics
// image, or whichever the terminal supports.
type Graphics string

// Graphics modes. GraphicsAuto probes the terminal and shows an image only
// where Kitty Graphics with Unicode placeholders is known to work (Ghostty,
// kitty), GraphicsText never probes, GraphicsKitty forces the image without
// probing. The empty value behaves like GraphicsAuto in Decide; in Options it
// means a plain text chart without any terminal probing.
const (
	GraphicsAuto  Graphics = "auto"
	GraphicsText  Graphics = "text"
	GraphicsKitty Graphics = "kitty"
)

// Next is the mode the g key switches to: auto → text → kitty → auto.
func (g Graphics) Next() Graphics {
	switch g {
	case GraphicsText:
		return GraphicsKitty
	case GraphicsKitty:
		return GraphicsAuto
	default:
		return GraphicsText
	}
}

// Probe is the outcome of the Kitty Graphics probe (the a=q query the
// picture package sends at start-up).
type Probe int

// Probe outcomes.
const (
	// ProbePending: the probe is running or has not been started.
	ProbePending Probe = iota
	// ProbeSupported: the terminal answered the query.
	ProbeSupported
	// ProbeNotDetected: the environment gave no hint of a Kitty-capable
	// terminal, so nothing was sent.
	ProbeNotDetected
	// ProbeTimeout: the query was sent but nothing came back; usually a
	// multiplexer without passthrough.
	ProbeTimeout
	// ProbeSkipped: the probe can no longer run in this process, because
	// the program started with a forced mode.
	ProbeSkipped
)

// String implements fmt.Stringer.
func (p Probe) String() string {
	switch p {
	case ProbePending:
		return "pending"
	case ProbeSupported:
		return "supported"
	case ProbeNotDetected:
		return "not detected"
	case ProbeTimeout:
		return "timeout"
	case ProbeSkipped:
		return "skipped"
	}
	return "probe(" + strconv.Itoa(int(p)) + ")"
}

// Env is the part of the process environment the mode decision looks at.
// The variables are hints, not proof: they survive neither ssh nor tmux
// reliably, so they only ever restrict a positive probe, never replace it.
type Env struct {
	Term               string // TERM
	TermProgram        string // TERM_PROGRAM
	TermProgramVersion string // TERM_PROGRAM_VERSION
	Tmux               bool   // TMUX is set
	// Ghostty is set when GHOSTTY_RESOURCES_DIR or GHOSTTY_BIN_DIR is
	// present. tmux replaces TERM and TERM_PROGRAM in its panes, but a
	// server started from Ghostty keeps these in its environment.
	Ghostty bool
}

// EnvFromOS reads Env from the process environment.
func EnvFromOS() Env { return EnvFrom(os.Getenv) }

// EnvFrom reads Env through getenv.
func EnvFrom(getenv func(string) string) Env {
	return Env{
		Term:               getenv("TERM"),
		TermProgram:        getenv("TERM_PROGRAM"),
		TermProgramVersion: getenv("TERM_PROGRAM_VERSION"),
		Tmux:               getenv("TMUX") != "",
		Ghostty:            getenv("GHOSTTY_RESOURCES_DIR") != "" || getenv("GHOSTTY_BIN_DIR") != "",
	}
}

// placeholders reports whether the environment points at a terminal that
// renders Kitty Graphics through Unicode placeholders: Ghostty, kitty and
// herdr, a multiplexer that draws placeholders itself and forwards the
// image to its outer terminal. WezTerm and Konsole answer the probe but
// draw placeholders as garbage.
func (e Env) placeholders() bool {
	switch e.Term {
	case "xterm-kitty", "xterm-ghostty":
		return true
	}
	switch e.TermProgram {
	case "ghostty", "kitty", "herdr":
		return true
	}
	return e.Tmux && e.Ghostty
}

// ownProbe reports whether the chart has to send the Kitty query itself.
// The picture package sends its probe only to terminals it recognizes by
// their environment; a herdr pane (TERM_PROGRAM=herdr, TERM=xterm-256color,
// no Ghostty variables) is not among them and would resolve to "not
// detected" without a byte on the wire. herdr answers the query only while
// it has Kitty Graphics on and an outer terminal that can draw them, so the
// query stays the gate. In tmux the package probes anyway, wrapped in
// passthrough, and herdr is not visible there.
func (e Env) ownProbe() bool {
	return e.TermProgram == "herdr" && !e.Tmux
}

// name is how the status line calls the terminal.
func (e Env) name() string {
	switch {
	case e.TermProgram != "":
		return e.TermProgram
	case e.Term != "":
		return e.Term
	}
	return "this terminal"
}

// Decision is the result of Decide: whether to draw an image, and the reason
// for the status line when it is not the obvious one.
type Decision struct {
	// Image is true when the chart is to be drawn as an image.
	Image bool
	// Reason explains a text chart in auto mode ("not detected"), or marks a
	// forced image ("forced"). Empty for plain text mode and for an image
	// the probe approved.
	Reason string
}

// Label is the status line text: "image chart (kitty)" once the image is
// on screen (shown), "text chart" with the reason in brackets otherwise.
// While an image is decided but not yet placed, the text chart is on screen
// and the label says so.
func (d Decision) Label(shown bool) string {
	switch {
	case d.Image && shown && d.Reason != "":
		return "image chart (kitty, " + d.Reason + ")"
	case d.Image && shown:
		return "image chart (kitty)"
	case d.Image:
		return "text chart (kitty: rendering…)"
	case d.Reason != "":
		return "text chart (kitty: " + d.Reason + ")"
	}
	return "text chart"
}

// ghosttyPlaceholderVersion is the first Ghostty release whose placeholders
// work inside tmux (ghostty#13056).
const ghosttyPlaceholderMajor, ghosttyPlaceholderMinor = 1, 4

// Decide is the mode decision table: text never draws an image, kitty always
// does, auto draws one only when the probe succeeded and the environment
// names a terminal with working placeholders (Ghostty, kitty, herdr); in
// tmux additionally not for Ghostty before 1.4.
func Decide(mode Graphics, probe Probe, env Env) Decision {
	switch mode {
	case GraphicsText:
		return Decision{}
	case GraphicsKitty:
		return Decision{Image: true, Reason: "forced"}
	}
	switch probe {
	case ProbePending:
		return Decision{Reason: "probing…"}
	case ProbeNotDetected:
		return Decision{Reason: "not detected"}
	case ProbeTimeout:
		switch {
		case env.Tmux:
			return Decision{Reason: "no answer; tmux allow-passthrough?"}
		case env.TermProgram == "herdr":
			// herdr stops answering when terminal.kitty_graphics is off
			// or the outer terminal has no Kitty Graphics.
			return Decision{Reason: "no answer; herdr kitty_graphics off?"}
		}
		return Decision{Reason: "no answer"}
	case ProbeSkipped:
		return Decision{Reason: "not probed"}
	}
	switch {
	case env.Tmux && env.TermProgram == "ghostty" && versionBefore(env.TermProgramVersion, ghosttyPlaceholderMajor, ghosttyPlaceholderMinor):
		return Decision{Reason: "ghostty " + env.TermProgramVersion + " in tmux needs 1.4"}
	case env.placeholders():
		return Decision{Image: true}
	case env.Tmux:
		return Decision{Reason: "outer terminal unknown"}
	}
	return Decision{Reason: "no placeholders in " + env.name()}
}

// versionBefore reports whether v ("1.3.1", "1.4.0-main+abc") is older than
// major.minor. Unparsable or empty versions are not "before": when the
// version is unknown, the terminal gets the benefit of the doubt.
func versionBefore(v string, major, minor int) bool {
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return false
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	mnr, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return maj < major || (maj == major && mnr < minor)
}
