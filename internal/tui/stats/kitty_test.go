package stats

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/chart"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// fixedProbe is a probe outcome tests inject.
func fixedProbe(p Probe) func() Probe { return func() Probe { return p } }

// newKitty builds a container for tests: Ghostty environment, probe p.
func newKitty(mode Graphics, p Probe) *KittyChart {
	env := ghostty
	return NewKittyChart(KittyOptions{Mode: mode, Env: &env, Probe: fixedProbe(p)})
}

// exec runs cmd to completion against c like a program would: batches and
// sequences are unpacked, raw terminal writes are collected instead of
// delivered, every other message goes to c.Update and its command runs in
// turn. It returns everything written to the terminal.
func exec(t *testing.T, c Chart, cmd tea.Cmd) []string {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch m := msg.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []string
		for _, sub := range m {
			out = append(out, exec(t, c, sub)...)
		}
		return out
	case tea.RawMsg:
		s, ok := m.Msg.(string)
		if !ok {
			t.Fatalf("raw message of type %T", m.Msg)
		}
		return []string{s}
	}
	// tea.Sequence yields an unexported slice of commands.
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
		var out []string
		for i := range v.Len() {
			out = append(out, exec(t, c, v.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	_, next := c.Update(msg)
	return exec(t, c, next)
}

// spec is a small chart spec with two series.
var spec = chart.Spec{
	Title: "events", Labels: []string{"Mon", "Tue", "Wed"},
	Series: []chart.Series{
		{Key: "a", Name: "alpha", Color: chart.Palette[0], Values: []float64{3, 5, 2}},
		{Key: "b", Name: "beta", Color: chart.Palette[1], Values: []float64{1, 0, 4}},
	},
}

// start inits the container, sizes it and hands it the spec, running every
// command; it returns the terminal writes.
func start(t *testing.T, c *KittyChart, cols, rows int) []string {
	t.Helper()
	out := exec(t, c, c.Init())
	out = append(out, exec(t, c, c.SetSize(cols, rows))...)
	return append(out, exec(t, c, c.SetSpec(spec))...)
}

func hasPlaceholder(s string) bool { return strings.ContainsRune(s, kitty.Placeholder) }

func joined(out []string) string { return strings.Join(out, "") }

// TestKittyChartTextIsSilent: text mode never writes to the terminal and
// draws the text chart.
func TestKittyChartTextIsSilent(t *testing.T) {
	c := newKitty(GraphicsText, ProbeSupported)
	out := start(t, c, 60, 12)
	out = append(out, exec(t, c, func() tea.Cmd { _, cmd := c.Update(uv.CellSizeEvent{Width: 9, Height: 18}); return cmd }())...)
	if len(out) != 0 {
		t.Errorf("text mode wrote to the terminal: %q", out)
	}
	if got, want := c.View(), chart.RenderText(spec, 60, 12); got != want {
		t.Errorf("view is not the text chart:\n%s", got)
	}
	if got := c.Mode(); got != "text chart" {
		t.Errorf("Mode() = %q", got)
	}
}

// TestKittyChartAutoFallsBack: a negative probe keeps the text chart and
// names the reason.
func TestKittyChartAutoFallsBack(t *testing.T) {
	tests := []struct {
		probe Probe
		want  string
	}{
		{ProbeNotDetected, "text chart (kitty: not detected)"},
		{ProbeTimeout, "text chart (kitty: no answer)"},
		{ProbePending, "text chart (kitty: probing…)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			c := newKitty(GraphicsAuto, tt.probe)
			if out := start(t, c, 60, 12); len(out) != 0 {
				t.Errorf("wrote to the terminal: %q", out)
			}
			if got := c.Mode(); got != tt.want {
				t.Errorf("Mode() = %q, want %q", got, tt.want)
			}
			if hasPlaceholder(c.View()) {
				t.Error("view shows placeholders")
			}
		})
	}
}

// newHerdr builds a container in a herdr pane with a short own-probe
// timeout; the picture package's state is still open, so the chart probes
// itself.
func newHerdr(mode Graphics) *KittyChart {
	env := herdr
	return NewKittyChart(KittyOptions{Mode: mode, Env: &env, Probe: fixedProbe(ProbePending), ProbeTimeout: time.Millisecond})
}

// probeAnswer is the terminal's reply to the chart's own Kitty query.
func probeAnswer(id int) uv.KittyGraphicsEvent {
	return uv.KittyGraphicsEvent{Options: kitty.Options{ID: id}, Payload: []byte("OK")}
}

// TestKittyChartOwnProbe: in a herdr pane the picture package would send
// nothing, so the chart sends the a=q query itself at Init and waits. The
// query names the probe's own image ID, no image goes out before an
// answer, and with the short test timeout the status line reports the
// missing answer with herdr's switch.
func TestKittyChartOwnProbe(t *testing.T) {
	c := newHerdr(GraphicsAuto)
	out := joined(start(t, c, 60, 12))
	for _, want := range []string{"\x1b_G", "a=q", "i=" + strconv.Itoa(probeImageID)} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal output lacks %q: %q", want, out)
		}
	}
	if strings.Contains(out, "a=T") {
		t.Errorf("image transmitted before the terminal answered: %q", out)
	}
	if got, want := c.Mode(), "text chart (kitty: no answer; herdr kitty_graphics off?)"; got != want {
		t.Errorf("Mode() = %q, want %q", got, want)
	}
	if hasPlaceholder(c.View()) {
		t.Error("view shows placeholders")
	}
}

// TestKittyChartOwnProbeAnswer: the terminal's answer to the chart's own
// query switches the image on, even after the timeout fired first (the
// input queue can deliver the answer late). Answers to other IDs are not
// ours.
func TestKittyChartOwnProbeAnswer(t *testing.T) {
	c := newHerdr(GraphicsAuto)
	start(t, c, 60, 12)

	_, cmd := c.Update(probeAnswer(DefaultImageID))
	if out := joined(exec(t, c, cmd)); strings.Contains(out, "a=T") || hasPlaceholder(c.View()) {
		t.Errorf("an answer to a foreign ID placed the image: %q", out)
	}

	_, cmd = c.Update(probeAnswer(probeImageID))
	out := joined(exec(t, c, cmd))
	for _, want := range []string{"\x1b[16t", "a=T", "U=1", "i=23", "c=60", "r=12"} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal output lacks %q", want)
		}
	}
	if !hasPlaceholder(c.View()) {
		t.Fatalf("view shows no placeholders:\n%s", c.View())
	}
	if got := c.Mode(); got != "image chart (kitty)" {
		t.Errorf("Mode() = %q", got)
	}
	// g cycles as in any other terminal: text deletes, kitty forces, auto
	// keeps the answer.
	exec(t, c, c.Toggle())
	exec(t, c, c.Toggle())
	exec(t, c, c.Toggle())
	if got := c.Mode(); got != "image chart (kitty)" || !hasPlaceholder(c.View()) {
		t.Errorf("after g g g: Mode() = %q, placeholders %v", got, hasPlaceholder(c.View()))
	}
}

// TestKittyChartOwnProbeOnlyInAuto: text and kitty mode never send the
// chart's own query either.
func TestKittyChartOwnProbeOnlyInAuto(t *testing.T) {
	for _, mode := range []Graphics{GraphicsText, GraphicsKitty} {
		t.Run(string(mode), func(t *testing.T) {
			c := newHerdr(mode)
			if out := joined(start(t, c, 60, 12)); strings.Contains(out, "a=q") {
				t.Errorf("%s mode sent the query: %q", mode, out)
			}
		})
	}
}

// TestKittyChartOwnProbeHonorsForced: a capability settled before the
// chart starts (NTCHARTS_KITTY, ForceKittyCapability) is final in herdr
// too: no query goes out, the settled state decides.
func TestKittyChartOwnProbeHonorsForced(t *testing.T) {
	tests := []struct {
		probe Probe
		want  string
	}{
		{ProbeNotDetected, "text chart (kitty: not detected)"},
		{ProbeSupported, "image chart (kitty)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			env := herdr
			c := NewKittyChart(KittyOptions{Mode: GraphicsAuto, Env: &env, Probe: fixedProbe(tt.probe), ProbeTimeout: time.Millisecond})
			if out := joined(start(t, c, 60, 12)); strings.Contains(out, "a=q") {
				t.Errorf("query sent despite a settled capability: %q", out)
			}
			if got := c.Mode(); got != tt.want {
				t.Errorf("Mode() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestKittyChartNoOwnProbeInGhostty: where the picture package probes, the
// chart does not add a query of its own.
func TestKittyChartNoOwnProbeInGhostty(t *testing.T) {
	c := newKitty(GraphicsAuto, ProbeNotDetected)
	if out := joined(start(t, c, 60, 12)); strings.Contains(out, "i="+strconv.Itoa(probeImageID)) {
		t.Errorf("own query sent in Ghostty: %q", out)
	}
}

// TestKittyChartImage: a positive probe in Ghostty places the image: the
// cell size is requested, the PNG goes out in Kitty APC chunks with the
// stable image ID and Unicode placeholders, and the view shows the
// placeholder grid of the chart area.
func TestKittyChartImage(t *testing.T) {
	c := newKitty(GraphicsAuto, ProbeSupported)
	out := joined(start(t, c, 60, 12))
	if !strings.Contains(out, "\x1b[16t") {
		t.Error("cell size was not requested")
	}
	for _, want := range []string{"\x1b_G", "a=T", "U=1", "i=23", "f=100", "c=60", "r=12"} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal output lacks %q", want)
		}
	}
	v := c.View()
	if !hasPlaceholder(v) {
		t.Fatalf("view shows no placeholders:\n%s", v)
	}
	if lines := strings.Split(v, "\n"); len(lines) != 12 {
		t.Errorf("view has %d lines, want 12", len(lines))
	}
	if got := c.Mode(); got != "image chart (kitty)" {
		t.Errorf("Mode() = %q", got)
	}
	// The PNG has the pixel size of the cell area at the default cell size.
	if w, h := pngSize(t, out); w != 600 || h != 240 {
		t.Errorf("png is %d×%d, want 600×240", w, h)
	}
}

// TestKittyChartCellSize: the terminal's cell size answer re-renders the
// image at the real pixel size.
func TestKittyChartCellSize(t *testing.T) {
	c := newKitty(GraphicsAuto, ProbeSupported)
	start(t, c, 50, 10)
	_, cmd := c.Update(uv.CellSizeEvent{Width: 14, Height: 28})
	out := joined(exec(t, c, cmd))
	if !strings.Contains(out, "a=T") {
		t.Fatal("no image was transmitted after the cell size arrived")
	}
	if w, h := pngSize(t, out); w != 700 || h != 280 {
		t.Errorf("png is %d×%d, want 700×280", w, h)
	}
	if !hasPlaceholder(c.View()) {
		t.Error("view lost the placeholders")
	}
	// A new spec re-renders at the known cell size without asking again.
	out = joined(exec(t, c, c.SetSpec(spec)))
	if strings.Contains(out, "\x1b[16t") {
		t.Error("cell size requested twice")
	}
	if w, h := pngSize(t, out); w != 700 || h != 280 {
		t.Errorf("png after SetSpec is %d×%d, want 700×280", w, h)
	}
}

// TestKittyChartToggle cycles the modes with the image on screen: text
// deletes the image, kitty forces it back, auto restores the probe's
// verdict.
func TestKittyChartToggle(t *testing.T) {
	c := newKitty(GraphicsAuto, ProbeSupported)
	start(t, c, 60, 12)

	out := joined(exec(t, c, c.Toggle()))
	if !strings.Contains(out, "a=d") {
		t.Errorf("switching to text did not delete the image: %q", out)
	}
	if got := c.Mode(); got != "text chart" || hasPlaceholder(c.View()) {
		t.Errorf("after g: Mode() = %q, placeholders %v", got, hasPlaceholder(c.View()))
	}

	out = joined(exec(t, c, c.Toggle()))
	if !strings.Contains(out, "a=T") {
		t.Errorf("forcing kitty did not transmit: %q", out)
	}
	if got := c.Mode(); got != "image chart (kitty, forced)" || !hasPlaceholder(c.View()) {
		t.Errorf("after g g: Mode() = %q, placeholders %v", got, hasPlaceholder(c.View()))
	}

	exec(t, c, c.Toggle())
	if got := c.Mode(); got != "image chart (kitty)" || !hasPlaceholder(c.View()) {
		t.Errorf("after g g g: Mode() = %q, placeholders %v", got, hasPlaceholder(c.View()))
	}
	if got := c.Graphics(); got != GraphicsAuto {
		t.Errorf("Graphics() = %q", got)
	}
}

// TestKittyChartForcedThenAuto: a run that started with --graphics kitty
// never probed; going back to auto says so.
func TestKittyChartForcedThenAuto(t *testing.T) {
	c := newKitty(GraphicsKitty, ProbePending)
	out := joined(start(t, c, 60, 12))
	if !strings.Contains(out, "a=T") || !hasPlaceholder(c.View()) {
		t.Fatalf("forced mode shows no image; Mode() = %q", c.Mode())
	}
	if got := c.Mode(); got != "image chart (kitty, forced)" {
		t.Errorf("Mode() = %q", got)
	}
	out = joined(exec(t, c, c.Toggle()))
	if !strings.Contains(out, "a=d") || c.Mode() != "text chart (kitty: not probed)" {
		t.Errorf("after g: Mode() = %q, output %q", c.Mode(), out)
	}
}

// TestKittyChartSizeChangeInText: the text chart follows the size while the
// image is off, and the image is rendered at the latest size when it comes
// on.
func TestKittyChartSizeChangeInText(t *testing.T) {
	c := newKitty(GraphicsText, ProbeSupported)
	start(t, c, 60, 12)
	exec(t, c, c.SetSize(40, 10))
	if got, want := c.View(), chart.RenderText(spec, 40, 10); got != want {
		t.Error("text chart did not follow the size")
	}
	out := joined(exec(t, c, c.Toggle())) // kitty
	if !strings.Contains(out, "c=40") || !strings.Contains(out, "r=10") {
		t.Errorf("image not placed at the latest size: %q", out)
	}
}

// pngSize decodes the transmitted image's header out of the APC chunks in
// out and returns its pixel size.
func pngSize(t *testing.T, out string) (w, h int) {
	t.Helper()
	var payload strings.Builder
	collecting := false
	for _, chunk := range strings.Split(out, "\x1b\\") {
		i := strings.LastIndex(chunk, "\x1b_G")
		if i < 0 {
			continue
		}
		ctrl, data, _ := strings.Cut(chunk[i+3:], ";")
		if strings.Contains(ctrl, "a=T") {
			payload.Reset()
			collecting = true
		}
		if !collecting {
			continue
		}
		payload.WriteString(data)
		if !strings.Contains(ctrl, "m=1") {
			collecting = false
		}
	}
	raw, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return cfg.Width, cfg.Height
}

// TestGraphicsOption: Options.Graphics selects the container.
func TestGraphicsOption(t *testing.T) {
	if _, ok := New(Options{}).chart.(*TextChart); !ok {
		t.Error("empty Graphics does not give a TextChart")
	}
	for _, g := range []Graphics{GraphicsAuto, GraphicsText, GraphicsKitty} {
		c, ok := New(Options{Graphics: g}).chart.(*KittyChart)
		if !ok || c.Graphics() != g {
			t.Errorf("Graphics %q gives %T", g, New(Options{Graphics: g}).chart)
		}
	}
}

// TestGraphicsFrames pins the status line of the view for the modes and the
// g cycle with the probe forced to "not detected". The frames of the kitty
// mode are a placeholder grid and are checked in TestKittyChartImage.
func TestGraphicsFrames(t *testing.T) {
	tests := []struct {
		name string
		mode Graphics
		keys []string
	}{
		{"Auto", GraphicsAuto, nil},
		{"Text", GraphicsText, nil},
		{"AutoToText", GraphicsAuto, []string{"g"}},
		{"AutoToKittyToAuto", GraphicsAuto, []string{"g", "g", "g"}},
		{"TextToKittyToAuto", GraphicsText, []string{"g", "g"}},
		{"NoStoreToggles", GraphicsAuto, []string{"g"}},
	}
	st, _ := fixturedb.Activity(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var l Loader = st
			if strings.HasPrefix(tt.name, "NoStore") {
				l = nil
			}
			env := ghostty
			c := NewKittyChart(KittyOptions{Mode: tt.mode, Env: &env, Probe: fixedProbe(ProbeNotDetected)})
			m := New(Options{Loader: l, Now: clock, Location: testfixture.ActivityLocation, Chart: c})
			m = drive(t, m, tt.keys...)
			golden.RequireEqual(t, tuitest.Frame(m))
		})
	}
}

// TestGraphicsProgram runs the view in a test program with the container in
// kitty mode: the image is placed through the program's own command loop,
// including the sequence of transfer and placeholder grid.
func TestGraphicsProgram(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	env := ghostty
	c := NewKittyChart(KittyOptions{Mode: GraphicsKitty, Env: &env, Probe: fixedProbe(ProbeNotDetected)})
	tm := tuitest.New(t, New(Options{Loader: st, Now: clock, Location: testfixture.ActivityLocation, Chart: c}))
	tuitest.WaitFor(t, tm, "image chart (kitty, forced)")
	final := tuitest.Final(t, tm).(Model)
	if f := tuitest.Frame(final); !hasPlaceholder(f) || !strings.Contains(f, "image chart (kitty, forced)") {
		t.Errorf("frame:\n%s", f)
	}
}
