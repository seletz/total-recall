package stats

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/NimbleMarkets/ntcharts/picture/chartpicture/v2"
	"github.com/NimbleMarkets/ntcharts/v2/picture"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/seletz/total-recall/internal/tui/chart"
)

// DefaultImageID is the Kitty image ID of the chart. It stays the same for
// the life of the program, so every new picture replaces the previous one
// in the terminal instead of piling up.
const DefaultImageID = 23

// probeImageID is the image ID of the chart's own Kitty query (see
// Env.ownProbe). The terminal echoes it in its answer, which is how the
// answer is told apart from the picture package's probe and the chart.
const probeImageID = 42

// defaultProbeTimeout is how long the chart's own probe waits for an
// answer; the same as the picture package's probe.
const defaultProbeTimeout = 250 * time.Millisecond

// probeTimeoutMsg marks the end of the chart's own probe window.
type probeTimeoutMsg struct{}

// GraphicsToggler is implemented by chart containers that can switch their
// rendering; the g key of the view calls it.
type GraphicsToggler interface {
	// Toggle moves to the next Graphics mode and returns the commands that
	// draw or clear the image.
	Toggle() tea.Cmd
}

// KittyOptions configure a KittyChart.
type KittyOptions struct {
	// Mode is the starting mode; empty means GraphicsAuto.
	Mode Graphics
	// Env is the environment the decision looks at; nil means the process
	// environment.
	Env *Env
	// Probe reads the probe outcome; nil reads the picture package's
	// process-wide state. Tests inject a fixed answer.
	Probe func() Probe
	// ImageID is the Kitty image ID; 0 means DefaultImageID.
	ImageID int
	// ProbeTimeout is how long the chart's own probe waits for the
	// terminal; 0 means 250 ms. Tests shorten it.
	ProbeTimeout time.Duration
}

// KittyChart is a chart container that draws the spec as a Kitty Graphics
// image where the terminal allows it, and as a text chart everywhere else.
//
// The image side is a chartpicture.Model: it renders the bar chart to a PNG
// off the main loop, encodes it as Kitty APC chunks, writes them with
// tea.Raw and shows Unicode placeholder cells in the view. The probe and
// the cell-size request are the picture package's; this container only
// starts them, reads their outcome and applies Decide. It shows the text
// chart until the probe is positive and the first image is placed, so the
// view never flickers or waits.
//
// The mode cycles with Toggle: auto → text → kitty → auto. Mode returns the
// status line label, see Decision.Label.
type KittyChart struct {
	opts KittyOptions
	mode Graphics
	env  Env

	text *TextChart
	pic  chartpicture.Model
	cell chart.CellSize // zero until the terminal answers CSI 16 t

	spec       chart.Spec
	hasSpec    bool
	cols, rows int

	probe         Probe // last observed outcome; ProbePending until resolved
	probeStarted  bool  // this container started the probe (its own or the picture package's)
	ownProbe      bool  // the probe is the chart's own query (Env.ownProbe), not the picture package's
	cellRequested bool
	decision      Decision
}

// NewKittyChart returns the container in opts.Mode. Nothing is sent to the
// terminal before Init.
func NewKittyChart(opts KittyOptions) *KittyChart {
	if opts.Mode == "" {
		opts.Mode = GraphicsAuto
	}
	if opts.ImageID == 0 {
		opts.ImageID = DefaultImageID
	}
	if opts.Probe == nil {
		opts.Probe = readProbe
	}
	if opts.ProbeTimeout == 0 {
		opts.ProbeTimeout = defaultProbeTimeout
	}
	c := &KittyChart{opts: opts, mode: opts.Mode, text: NewTextChart()}
	if opts.Env != nil {
		c.env = *opts.Env
	} else {
		c.env = EnvFromOS()
	}
	// The chart probes itself only while the picture package's state is
	// still open; a capability forced beforehand (NTCHARTS_KITTY, tests)
	// is final for both.
	c.ownProbe = c.env.ownProbe() && opts.Probe() == ProbePending
	c.pic = c.newPicture()
	c.decision = Decide(c.mode, c.probe, c.env)
	return c
}

// readProbe maps the picture package's process-wide capability state to a
// Probe.
func readProbe() Probe {
	switch err := picture.KittyUnavailable(); err {
	case nil:
		return ProbeSupported
	case picture.ErrKittyProbePending:
		return ProbePending
	case picture.ErrKittyProbeTimeout:
		return ProbeTimeout
	default:
		return ProbeNotDetected
	}
}

// capability is the picture state to restore for a resolved probe.
func (p Probe) capability() picture.KittyCapability {
	switch p {
	case ProbeSupported:
		return picture.KittyCapabilitySupported
	case ProbePending:
		return picture.KittyCapabilityUnknown
	}
	return picture.KittyCapabilityUnsupported
}

// newPicture builds the picture model for the current cell size. The cell
// size is fixed at construction in chartpicture, so a new terminal answer
// means a new model; the image ID stays, so the terminal replaces the old
// picture.
func (c *KittyChart) newPicture() chartpicture.Model {
	cell := c.cellOrDefault()
	m := chartpicture.NewWithConfig(chartpicture.Config{
		KittyID: c.opts.ImageID, CellWidthPx: cell.Width, CellHeightPx: cell.Height,
	})
	// The embedded picture scales the PNG to its own idea of the cell size;
	// telling it ours keeps both at the same pixels, so nothing is rescaled.
	_ = m.Update(uv.CellSizeEvent{Width: cell.Width, Height: cell.Height})
	return m
}

func (c *KittyChart) cellOrDefault() chart.CellSize {
	w, h := chart.PixelSize(1, 1, c.cell)
	return chart.CellSize{Width: w, Height: h}
}

// Init implements Chart. In auto mode it starts the probe; in kitty mode it
// forces the capability and asks for the cell size; in text mode it does
// nothing, so a text-only run never writes a query to the terminal.
func (c *KittyChart) Init() tea.Cmd {
	switch c.mode {
	case GraphicsAuto:
		return tea.Batch(c.startProbe(), c.sync())
	case GraphicsKitty:
		return c.sync()
	}
	return nil
}

// startProbe starts the probe once: the picture package's for terminals it
// recognizes, the chart's own query where it would send nothing (herdr).
// When the package's probe already ran (or can no longer run) its state is
// read as is.
func (c *KittyChart) startProbe() tea.Cmd {
	if c.probeStarted {
		return nil
	}
	c.probeStarted = true
	if c.ownProbe {
		return c.queryKitty()
	}
	return picture.QueryKittySupport()
}

// queryKitty sends the chart's own Kitty query, an a=q transmit of one RGB
// pixel that a Kitty-aware terminal acknowledges with the same ID, and
// starts the timeout. Update resolves the probe from the answer or the
// timeout, and a late answer still wins.
func (c *KittyChart) queryKitty() tea.Cmd {
	query := fmt.Sprintf("\x1b_Ga=q,t=d,f=24,s=1,v=1,i=%d;AAAA\x1b\\", probeImageID)
	return tea.Batch(
		func() tea.Msg { return tea.RawMsg{Msg: query} },
		tea.Tick(c.opts.ProbeTimeout, func(time.Time) tea.Msg { return probeTimeoutMsg{} }),
	)
}

// observeProbe resolves the chart's own probe from msg.
func (c *KittyChart) observeProbe(msg tea.Msg) {
	switch m := msg.(type) {
	case uv.KittyGraphicsEvent:
		if m.Options.ID == probeImageID {
			c.probe = ProbeSupported
		}
	case probeTimeoutMsg:
		if c.probe == ProbePending {
			c.probe = ProbeTimeout
		}
	}
}

// Update implements Chart: it feeds the picture model, tracks the cell size
// and re-applies the decision after every message, since the probe result
// and the probe timeout arrive as messages.
func (c *KittyChart) Update(msg tea.Msg) (Chart, tea.Cmd) {
	if c.ownProbe {
		c.observeProbe(msg)
	}
	if ev, ok := msg.(uv.CellSizeEvent); ok && ev.Width > 0 && ev.Height > 0 {
		cell := chart.CellSize{Width: ev.Width, Height: ev.Height}
		if cell != c.cell {
			c.cell = cell
			if c.decision.Image {
				return c, c.rebuild()
			}
		}
	}
	cmd := c.pic.Update(msg)
	return c, tea.Batch(cmd, c.sync())
}

// sync observes the picture package's probe (the chart's own is resolved
// in Update) and applies the decision: entering image mode starts
// rendering, leaving it deletes the image.
func (c *KittyChart) sync() tea.Cmd {
	if c.mode == GraphicsAuto && !c.ownProbe {
		if p := c.opts.Probe(); p != ProbePending {
			c.probe = p
		}
	}
	d := Decide(c.mode, c.probe, c.env)
	was := c.decision.Image
	c.decision = d
	switch {
	case d.Image && (!was || c.pic.Mode() != picture.PictureKitty):
		// Newly decided, or decided before Init (a forced start).
		return c.rebuild()
	case !d.Image && was:
		return c.leaveImage()
	}
	return nil
}

// rebuild creates the picture model for the current cell size and, in image
// mode, feeds it the size and the spec and switches it to Kitty mode. The
// first cell-size request goes out here too, so a text-only session never
// asks.
func (c *KittyChart) rebuild() tea.Cmd {
	c.pic = c.newPicture()
	if !c.decision.Image {
		return nil
	}
	// The picture package refuses Kitty mode unless the capability is
	// known to be supported; the decision is ours, so tell it.
	picture.ForceKittyCapability(picture.KittyCapabilitySupported)
	var cmds []tea.Cmd
	if !c.cellRequested {
		c.cellRequested = true
		cmds = append(cmds, picture.RequestCellSize())
	}
	cmds = append(cmds, c.pic.SetSize(c.cols, c.rows))
	if c.hasSpec {
		cmds = append(cmds, c.pic.SetBarChartOption(chart.ToBarChartOptionAt(c.spec, c.cellOrDefault())))
	}
	if c.pic.Mode() != picture.PictureKitty {
		cmds = append(cmds, c.pic.Toggle())
	}
	return tea.Batch(cmds...)
}

// leaveImage switches the picture back to glyph mode, which deletes the
// placed image from the terminal.
func (c *KittyChart) leaveImage() tea.Cmd {
	if c.pic.Mode() == picture.PictureKitty {
		return c.pic.Toggle()
	}
	return nil
}

// Toggle implements GraphicsToggler: auto → text → kitty → auto. Returning
// to auto restores the probe outcome the picture package had before kitty
// was forced; when nothing was probed yet, the probe starts now. The
// chart's own probe keeps running across the cycle, its answer arrives in
// Update.
func (c *KittyChart) Toggle() tea.Cmd {
	c.mode = c.mode.Next()
	var cmd tea.Cmd
	if c.mode == GraphicsAuto {
		picture.ForceKittyCapability(c.probe.capability())
		cmd = c.startProbe()
		if cmd == nil && c.probe == ProbePending && !c.ownProbe {
			c.probe = ProbeSkipped
		}
	}
	return tea.Batch(cmd, c.sync())
}

// SetSpec implements Chart.
func (c *KittyChart) SetSpec(spec chart.Spec) tea.Cmd {
	c.spec, c.hasSpec = spec, true
	c.text.SetSpec(spec)
	if !c.decision.Image {
		return nil
	}
	return c.pic.SetBarChartOption(chart.ToBarChartOptionAt(spec, c.cellOrDefault()))
}

// SetSize implements Chart.
func (c *KittyChart) SetSize(width, height int) tea.Cmd {
	c.cols, c.rows = width, height
	c.text.SetSize(width, height)
	if !c.decision.Image {
		return nil
	}
	return c.pic.SetSize(width, height)
}

// shown reports whether the placeholder grid of the image is what View
// returns right now.
func (c *KittyChart) shown() (string, bool) {
	if !c.decision.Image || c.pic.Mode() != picture.PictureKitty || c.pic.Err() != nil {
		return "", false
	}
	v := c.pic.View().Content
	if !strings.ContainsRune(v, kitty.Placeholder) {
		// Not placed yet: the picture would draw half-blocks meanwhile,
		// the text chart is the better interim.
		return "", false
	}
	return v, true
}

// View implements Chart: the placeholder grid once the image is placed,
// the text chart until then and in text mode.
func (c *KittyChart) View() string {
	if v, ok := c.shown(); ok {
		return v
	}
	return c.text.View()
}

// Mode implements Chart.
func (c *KittyChart) Mode() string {
	_, ok := c.shown()
	if c.decision.Image && c.pic.Err() != nil {
		return "text chart (kitty: render error)"
	}
	return c.decision.Label(ok)
}

// Graphics is the current mode.
func (c *KittyChart) Graphics() Graphics { return c.mode }
