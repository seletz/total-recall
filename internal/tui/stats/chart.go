package stats

import (
	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/tui/chart"
)

// Chart is the chart area of the view, a swappable container. The model
// hands it the spec whenever the data or the metric changes and the size
// whenever the layout changes, and draws its View in the chart area. Init
// and Update let an implementation run commands and react to messages, so
// a renderer that needs the terminal (Kitty Graphics: probe, cell size,
// image transfer) can live entirely inside its container; the model never
// learns which renderer is active. Mode names the renderer in the status
// line, e.g. "text chart".
//
// The zero model uses TextChart; another container comes in through
// Options.Chart.
type Chart interface {
	// Init starts the container, like tea.Model.Init.
	Init() tea.Cmd
	// Update receives every message the model does not consume itself:
	// no key presses, no window sizes (those arrive via SetSize) and no
	// load results.
	Update(msg tea.Msg) (Chart, tea.Cmd)
	// SetSpec sets the chart to draw.
	SetSpec(spec chart.Spec)
	// SetSize sets the area in cells; View must fit into it.
	SetSize(width, height int)
	// View draws the chart area: at most height lines of at most width
	// cells. Shorter output is padded by the model.
	View() string
	// Mode is the renderer's name for the status line.
	Mode() string
}

// TextChart draws the spec with chart.RenderText.
type TextChart struct {
	spec          chart.Spec
	width, height int
}

// NewTextChart returns an empty text chart container.
func NewTextChart() *TextChart { return &TextChart{} }

// Init implements Chart; the text chart needs no commands.
func (*TextChart) Init() tea.Cmd { return nil }

// Update implements Chart; the text chart ignores messages.
func (c *TextChart) Update(tea.Msg) (Chart, tea.Cmd) { return c, nil }

// SetSpec implements Chart.
func (c *TextChart) SetSpec(spec chart.Spec) { c.spec = spec }

// SetSize implements Chart.
func (c *TextChart) SetSize(width, height int) { c.width, c.height = width, height }

// View implements Chart: exactly height lines, or "" when the area is too
// small for a chart.
func (c *TextChart) View() string { return chart.RenderText(c.spec, c.width, c.height) }

// Mode implements Chart.
func (*TextChart) Mode() string { return "text chart" }
