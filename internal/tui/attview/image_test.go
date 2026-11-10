package attview

import (
	"image"
	"image/color"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/NimbleMarkets/ntcharts/v2/picture"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
	"github.com/seletz/total-recall/internal/tui/stats"
	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// kittyEnv is the environment of a terminal with working placeholders.
func kittyEnv(noColor bool) func(string) string {
	vars := map[string]string{"TERM": "xterm-ghostty", "TERM_PROGRAM": "ghostty"}
	if noColor {
		vars["NO_COLOR"] = "1"
	}
	return func(k string) string { return vars[k] }
}

func probe(p stats.Probe) func() stats.Probe { return func() stats.Probe { return p } }

func miniImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(40 * x), G: uint8(60 * y), B: 128, A: 255})
		}
	}
	return img
}

func TestImageDecision(t *testing.T) {
	tests := []struct {
		name    string
		mode    stats.Graphics
		env     func(string) string
		probe   stats.Probe
		want    bool
		wantWhy string
	}{
		{"text never draws", stats.GraphicsText, kittyEnv(false), stats.ProbeSupported, false, ""},
		{"auto with a positive probe", stats.GraphicsAuto, kittyEnv(false), stats.ProbeSupported, true, ""},
		{"auto while probing", stats.GraphicsAuto, kittyEnv(false), stats.ProbePending, false, "probing…"},
		{"auto, terminal not detected", stats.GraphicsAuto, kittyEnv(false), stats.ProbeNotDetected, false, "not detected"},
		{"auto, no answer", stats.GraphicsAuto, kittyEnv(false), stats.ProbeTimeout, false, "no answer"},
		{"auto, a terminal without placeholders", stats.GraphicsAuto, func(k string) string { return map[string]string{"TERM_PROGRAM": "WezTerm"}[k] }, stats.ProbeSupported, false, "no placeholders"},
		{"auto with NO_COLOR", stats.GraphicsAuto, kittyEnv(true), stats.ProbeSupported, false, "NO_COLOR"},
		{"kitty forced", stats.GraphicsKitty, kittyEnv(false), stats.ProbeNotDetected, true, "forced"},
		{"kitty forced with NO_COLOR", stats.GraphicsKitty, kittyEnv(true), stats.ProbeSupported, false, "NO_COLOR"},
		{"empty NO_COLOR is not set", stats.GraphicsAuto, func(k string) string {
			return map[string]string{"TERM": "xterm-ghostty", "NO_COLOR": ""}[k]
		}, stats.ProbeSupported, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iv := newImageView(tt.mode, tt.env, probe(tt.probe))
			d := iv.decide()
			if d.Image != tt.want || !strings.Contains(d.Reason, tt.wantWhy) {
				t.Errorf("decide = %+v, want image %v, reason containing %q", d, tt.want, tt.wantWhy)
			}
		})
	}
}

// apcPayload elides the base64 data of every chunk, whose bytes depend on the
// encoder, and keeps the control data, which is the protocol.
var apcPayload = regexp.MustCompile(`(\x1b_G[^;\x1b]*;)[A-Za-z0-9+/=]*(\x1b\\)`)

// escapes renders terminal writes readable: the escape character as ␛.
func escapes(writes []string) string {
	var b strings.Builder
	for i, w := range writes {
		w = apcPayload.ReplaceAllString(w, "${1}…${2}")
		b.WriteString(strings.ReplaceAll(w, "\x1b", "␛"))
		if i < len(writes)-1 {
			b.WriteString("\n")
		}
	}
	return b.String() + "\n"
}

// driver runs the commands of an imageView like a program would, and
// remembers the writes to the terminal.
type driver struct {
	t      *testing.T
	iv     *imageView
	writes []string
}

func (d *driver) run(cmd tea.Cmd) {
	d.t.Helper()
	d.writes = append(d.writes, tuitest.Exec(d.t, func(msg tea.Msg) tea.Cmd { return d.iv.update(msg) }, cmd)...)
}

func newDriver(t *testing.T, mode stats.Graphics, env func(string) string, p stats.Probe) *driver {
	t.Helper()
	picture.ForceKittyCapability(picture.KittyCapabilityUnknown)
	t.Cleanup(func() { picture.ForceKittyCapability(picture.KittyCapabilityUnknown) })
	iv := newImageView(mode, env, probe(p))
	return &driver{t: t, iv: iv}
}

func TestImageEscapesGolden(t *testing.T) {
	d := newDriver(t, stats.GraphicsAuto, kittyEnv(false), stats.ProbeSupported)
	d.iv.cellSize(8, 16)
	d.run(d.iv.show("sha-a", miniImage(8, 4), 4, 1))
	placed, ok := d.iv.placed("sha-a")
	if !ok || len(placed) != 1 || !strings.ContainsRune(placed[0], '\U0010eeee') {
		t.Fatalf("placed = %q, %v", placed, ok)
	}
	// Browsing on to something that is no image removes it.
	d.run(d.iv.clear())
	if _, ok := d.iv.placed("sha-a"); ok {
		t.Error("still placed after clear")
	}
	golden.RequireEqual(t, []byte(escapes(d.writes)))
}

func TestImageFallbackWithoutKitty(t *testing.T) {
	tests := []struct {
		name  string
		mode  stats.Graphics
		env   func(string) string
		probe stats.Probe
	}{
		{"text mode", stats.GraphicsText, kittyEnv(false), stats.ProbeSupported},
		{"not detected", stats.GraphicsAuto, kittyEnv(false), stats.ProbeNotDetected},
		{"no answer", stats.GraphicsAuto, kittyEnv(false), stats.ProbeTimeout},
		{"NO_COLOR", stats.GraphicsAuto, kittyEnv(true), stats.ProbeSupported},
		{"NO_COLOR with kitty forced", stats.GraphicsKitty, kittyEnv(true), stats.ProbeSupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriver(t, tt.mode, tt.env, tt.probe)
			d.run(d.iv.show("a", miniImage(8, 4), 4, 1))
			if len(d.writes) != 0 {
				t.Errorf("wrote to the terminal: %q", d.writes)
			}
			if _, ok := d.iv.placed("a"); ok {
				t.Error("placed without Kitty")
			}
		})
	}
}

func TestImageLifecycle(t *testing.T) {
	d := newDriver(t, stats.GraphicsKitty, kittyEnv(false), stats.ProbeSupported)
	d.iv.cellSize(8, 16)
	hasDelete := func(w []string) bool {
		for _, s := range w {
			if strings.Contains(s, "a=d") {
				return true
			}
		}
		return false
	}
	count := func(w []string, sub string) int {
		n := 0
		for _, s := range w {
			n += strings.Count(s, sub)
		}
		return n
	}
	step := func(name string, f func() tea.Cmd) (writes []string) {
		before := len(d.writes)
		d.run(f())
		writes = d.writes[before:]
		t.Logf("%s: %d writes", name, len(writes))
		return writes
	}

	w := step("show", func() tea.Cmd { return d.iv.show("a", miniImage(8, 4), 4, 1) })
	if count(w, "a=T") == 0 && count(w, "a=t") == 0 || hasDelete(w) {
		t.Errorf("show transmits one image and deletes nothing: %q", w)
	}
	// Browsing to another image replaces it under the same ID, no delete needed.
	w = step("next image", func() tea.Cmd { return d.iv.show("b", miniImage(6, 6), 4, 2) })
	if count(w, "a=T") == 0 && count(w, "a=t") == 0 {
		t.Errorf("the next image is transmitted: %q", w)
	}
	if _, ok := d.iv.placed("a"); ok {
		t.Error("the old image is still reported as placed")
	}
	// A resize re-places it; the new geometry removes the old placement first.
	w = step("resize", func() tea.Cmd { return d.iv.show("b", miniImage(6, 6), 6, 3) })
	if !hasDelete(w) || count(w, "a=T")+count(w, "a=t") == 0 {
		t.Errorf("resize deletes and places again: %q", w)
	}
	// Browsing on to a document removes it.
	w = step("clear", func() tea.Cmd { return d.iv.clear() })
	if !hasDelete(w) {
		t.Errorf("clear deletes the image: %q", w)
	}
	if got := d.iv.clear(); got != nil {
		t.Error("clear with nothing placed does nothing")
	}
	// Leaving the view with an image shown removes it too.
	d.run(d.iv.show("c", miniImage(4, 4), 2, 1))
	w = step("leave", func() tea.Cmd { return d.iv.clear() })
	if !hasDelete(w) {
		t.Errorf("leaving deletes the image: %q", w)
	}
}

func TestImageDecisionChangesWhileShown(t *testing.T) {
	// The probe answers late: the image is drawn as text until then, and a
	// positive answer places it.
	p := stats.ProbePending
	picture.ForceKittyCapability(picture.KittyCapabilityUnknown)
	t.Cleanup(func() { picture.ForceKittyCapability(picture.KittyCapabilityUnknown) })
	iv := newImageView(stats.GraphicsAuto, kittyEnv(false), func() stats.Probe { return p })
	iv.cellSize(8, 16)
	d := &driver{t: t, iv: iv}
	d.run(iv.show("a", miniImage(8, 4), 4, 1))
	if _, ok := iv.placed("a"); ok || len(d.writes) != 0 {
		t.Fatalf("drawn before the probe answered: %v %q", ok, d.writes)
	}
	p = stats.ProbeSupported
	d.run(iv.update(struct{}{})) // any message re-applies the decision
	if _, ok := iv.placed("a"); !ok {
		t.Error("not placed after the probe answered")
	}
}

// modelDriver drives the attachment view over the demo archive like a
// program would and collects its writes to the terminal.
type modelDriver struct {
	t      *testing.T
	m      Model
	writes []string
	back   bool
}

func newModelDriver(t *testing.T, opts Options, w, h int) *modelDriver {
	t.Helper()
	picture.ForceKittyCapability(picture.KittyCapabilityUnknown)
	t.Cleanup(func() { picture.ForceKittyCapability(picture.KittyCapabilityUnknown) })
	st, _ := fixturedb.Demo(t)
	opts.Loader, opts.Location = st, demo.Location
	d := &modelDriver{t: t, m: New(opts)}
	d.send(tea.WindowSizeMsg{Width: w, Height: h})
	sid, eid := demoEvent(t, st)
	d.send(Open{SessionID: sid, EventID: eid, Title: "Höhenprofil prüfen"})
	return d
}

func (d *modelDriver) send(msg tea.Msg) []string {
	d.t.Helper()
	before := len(d.writes)
	update := func(msg tea.Msg) tea.Cmd {
		if _, ok := msg.(Back); ok {
			d.back = true
			return nil
		}
		next, cmd := d.m.Update(msg)
		d.m = next.(Model)
		return cmd
	}
	d.writes = append(d.writes, tuitest.Exec(d.t, update, func() tea.Msg { return msg })...)
	return d.writes[before:]
}

func (d *modelDriver) frame() string { return frame(d.m) }

func anyContains(writes []string, sub string) bool {
	for _, w := range writes {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

func kittyOptions() Options {
	return Options{Images: stats.GraphicsAuto, Getenv: kittyEnv(false), Probe: probe(stats.ProbeSupported), External: &fakeOpener{}}
}

func TestViewDrawsTheImageWithKitty(t *testing.T) {
	d := newModelDriver(t, kittyOptions(), 100, 30)
	if !anyContains(d.writes, "a=T") || !anyContains(d.writes, "i=47") {
		t.Errorf("the image is transmitted: %q", d.writes)
	}
	got := d.frame()
	if !strings.ContainsRune(got, '\U0010eeee') {
		t.Errorf("no placeholder cells in the view:\n%s", got)
	}
	if strings.ContainsAny(got, "░▒▓█") {
		t.Errorf("the text image is drawn too:\n%s", got)
	}
	for _, want := range []string{"png 96×48", "image: kitty", "total-recall blob"} {
		if !strings.Contains(got, want) {
			t.Errorf("view lacks %q:\n%s", want, got)
		}
	}
	// Every line stays inside the width, the placeholders count one cell.
	for _, l := range strings.Split(d.m.View().Content, "\n") {
		if w := ansi.StringWidth(l); w > 100 {
			t.Errorf("line of %d cells: %q", w, l)
		}
	}
}

func TestViewRemovesTheImage(t *testing.T) {
	d := newModelDriver(t, kittyOptions(), 100, 30)
	steps := []struct {
		name       string
		msg        tea.Msg
		wantDelete bool
		wantImage  bool // placeholder cells in the view afterwards
		wantNew    bool // a new transmission
	}{
		{"browse to the document", tuitest.Key("right"), true, false, false},
		{"back to the image", tuitest.Key("left"), false, true, true},
		{"resize", tea.WindowSizeMsg{Width: 70, Height: 20}, true, true, true},
		{"same size again", tea.WindowSizeMsg{Width: 70, Height: 20}, false, true, false},
	}
	for _, st := range steps {
		w := d.send(st.msg)
		if got := anyContains(w, "a=d"); got != st.wantDelete {
			t.Errorf("%s: delete = %v, want %v (%q)", st.name, got, st.wantDelete, w)
		}
		if got := anyContains(w, "a=T"); got != st.wantNew {
			t.Errorf("%s: new transmission = %v, want %v", st.name, got, st.wantNew)
		}
		if got := strings.ContainsRune(d.frame(), '\U0010eeee'); got != st.wantImage {
			t.Errorf("%s: placeholders in the view = %v, want %v", st.name, got, st.wantImage)
		}
	}
	// Leaving removes it: esc sends Back after the delete, Leave deletes too.
	w := d.send(tuitest.Key("esc"))
	if !anyContains(w, "a=d") || !d.back {
		t.Errorf("esc: writes %q, back %v", w, d.back)
	}
	d2 := newModelDriver(t, kittyOptions(), 100, 30)
	w = tuitest.Exec(t, func(msg tea.Msg) tea.Cmd { return nil }, d2.m.Leave())
	if !anyContains(w, "a=d") {
		t.Errorf("Leave: %q", w)
	}
	if w := tuitest.Exec(t, func(msg tea.Msg) tea.Cmd { return nil }, d2.m.Leave()); len(w) != 0 {
		t.Errorf("a second Leave writes again: %q", w)
	}
}

func TestViewFallsBackToText(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want string // in the status line
	}{
		{"images = text", Options{Images: stats.GraphicsText, Getenv: kittyEnv(false), Probe: probe(stats.ProbeSupported)}, "image: text"},
		{"probe says no", Options{Getenv: kittyEnv(false), Probe: probe(stats.ProbeNotDetected)}, "image: text (not detected)"},
		{"no answer", Options{Getenv: kittyEnv(false), Probe: probe(stats.ProbeTimeout)}, "image: text (no answer)"},
		{"NO_COLOR", Options{Getenv: kittyEnv(true), Probe: probe(stats.ProbeSupported)}, "image: text (NO_COLOR)"},
		{"NO_COLOR beats images = kitty", Options{Images: stats.GraphicsKitty, Getenv: kittyEnv(true), Probe: probe(stats.ProbeSupported)}, "image: text (NO_COLOR)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.External = &fakeOpener{}
			d := newModelDriver(t, tt.opts, 100, 30)
			if len(d.writes) != 0 {
				t.Errorf("wrote to the terminal: %q", d.writes)
			}
			got := d.frame()
			if !strings.Contains(got, tt.want) || !strings.ContainsAny(got, "░▒▓█") || strings.ContainsRune(got, '\U0010eeee') {
				t.Errorf("view (want %q, text image, no placeholders):\n%s", tt.want, got)
			}
		})
	}
}

func TestViewInitProbesOnlyWhenItCanMatter(t *testing.T) {
	for name, tt := range map[string]struct {
		opts Options
		want bool
	}{
		"auto":     {Options{Images: stats.GraphicsAuto, Getenv: kittyEnv(false)}, true},
		"kitty":    {Options{Images: stats.GraphicsKitty, Getenv: kittyEnv(false)}, true},
		"text":     {Options{Images: stats.GraphicsText, Getenv: kittyEnv(false)}, false},
		"NO_COLOR": {Options{Getenv: kittyEnv(true)}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := New(tt.opts).Init() != nil; got != tt.want {
				// The probe runs once per process; a nil command can also mean
				// it already ran in an earlier test, so only the "no" answers
				// are exact.
				if !tt.want {
					t.Errorf("Init() starts a probe")
				}
			}
		})
	}
}
