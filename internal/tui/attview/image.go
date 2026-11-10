package attview

import (
	"image"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/NimbleMarkets/ntcharts/v2/picture"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/seletz/total-recall/internal/imgtext"
	"github.com/seletz/total-recall/internal/tui/stats"
)

// imageID is the Kitty image ID of the attachment view. It stays the same
// for the life of the program, so every new image replaces the previous one
// in the terminal instead of piling up; the stats chart uses another one.
const imageID = 47

// defaultCellW and defaultCellH are the cell size in pixels assumed until
// the terminal answers the cell-size request.
const defaultCellW, defaultCellH = 8, 16

// imageView draws the image of the marked attachment through the Kitty
// Graphics protocol where the terminal can, and says when it cannot (the
// attachment view then shows the text image of package imgtext).
//
// The picture side is the ntcharts picture.Model the stats chart uses: it
// scales the image to the cells, encodes it as APC chunks off the main
// loop, writes them with tea.Raw and returns Unicode placeholder cells for
// the view. The mode decision is the chart's (stats.Decide) over the same
// probe, so a terminal is asked once; NO_COLOR comes first, because the
// placeholder cells carry the image ID in their foreground colour and a
// renderer that drops colours would break them.
//
// An imageView is shared by the copies of the model (it is a pointer): the
// model is a value, the terminal state is one.
type imageView struct {
	mode    stats.Graphics
	env     stats.Env
	noColor bool
	probe   func() stats.Probe

	pic           picture.Model
	cellRequested bool

	// want is the image to show (nil: none), at cols×rows cells; key says
	// which attachment it is. placedKey is the key of the image handed to
	// the picture model, "" when none.
	want       image.Image
	key        string
	cols, rows int
	placedKey  string

	// decoded caches the last decoded image by attachment key.
	decodedKey string
	decoded    image.Image
}

// newImageView returns the view for a mode, reading the environment through
// getenv (nil: the process environment) and the probe outcome through probe
// (nil: the picture package's).
func newImageView(mode stats.Graphics, getenv func(string) string, probe func() stats.Probe) *imageView {
	if mode == "" {
		mode = stats.GraphicsAuto
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	if probe == nil {
		probe = stats.ReadProbe
	}
	iv := &imageView{mode: mode, env: stats.EnvFrom(getenv), noColor: getenv("NO_COLOR") != "", probe: probe}
	iv.pic = picture.NewWithConfig(picture.Config{KittyID: imageID})
	iv.pic.SetCellPixelSize(defaultCellW, defaultCellH)
	return iv
}

// decide is the mode decision: an image only where the terminal can draw it.
func (iv *imageView) decide() stats.Decision {
	if iv.noColor {
		return stats.Decision{Reason: "NO_COLOR"}
	}
	return stats.Decide(iv.mode, iv.probe(), iv.env)
}

// label says in the status line how the marked image is drawn: "kitty" once
// the terminal has it, else "text" with the reason when there is one.
func (iv *imageView) label(key string) string {
	if _, ok := iv.placed(key); ok {
		return "kitty"
	}
	d := iv.decide()
	switch {
	case d.Image:
		return "text (kitty: rendering…)"
	case d.Reason != "":
		return "text (" + d.Reason + ")"
	}
	return "text"
}

// init starts the probe where it can matter; the picture package runs it
// once per process, so the stats chart's start and this one share it.
func (iv *imageView) init() tea.Cmd {
	if iv.mode == stats.GraphicsText || iv.noColor {
		return nil
	}
	return picture.QueryKittySupport()
}

// cellSize sets the size of a cell in pixels, as the terminal reports it.
func (iv *imageView) cellSize(w, h int) tea.Cmd { return iv.pic.SetCellPixelSize(w, h) }

// update feeds a message to the picture model (the probe answer, the cell
// size, an encoded frame) and applies the decision again, since the probe
// result arrives as a message.
func (iv *imageView) update(msg tea.Msg) tea.Cmd {
	cmd := iv.pic.Update(msg)
	return tea.Batch(cmd, iv.sync())
}

// show makes img, of the attachment key, the image to draw in cols×rows
// cells.
func (iv *imageView) show(key string, img image.Image, cols, rows int) tea.Cmd {
	iv.want, iv.key, iv.cols, iv.rows = img, key, cols, rows
	return iv.sync()
}

// clear removes the image: from the terminal when it was placed, from the
// plan in every case. It does nothing when nothing is placed.
func (iv *imageView) clear() tea.Cmd {
	iv.want, iv.key = nil, ""
	return iv.sync()
}

// sync brings the picture model in line with the plan and the decision:
// entering image mode requests the cell size once and switches the model to
// Kitty, a new image replaces the old one under the same ID, and leaving
// (nothing to show, or the decision turned against it) deletes the image in
// the terminal.
func (iv *imageView) sync() tea.Cmd {
	draw := iv.decide().Image && iv.want != nil
	if !draw {
		if iv.placedKey == "" {
			return nil
		}
		iv.placedKey = ""
		return iv.pic.SetImage(nil)
	}
	var cmds []tea.Cmd
	if iv.pic.Mode() != picture.PictureKitty {
		// The picture package refuses Kitty mode unless the capability is
		// known to be supported; the decision is ours, so tell it.
		picture.ForceKittyCapability(picture.KittyCapabilitySupported)
		if !iv.cellRequested {
			iv.cellRequested = true
			cmds = append(cmds, picture.RequestCellSize())
		}
		cmds = append(cmds, iv.pic.Toggle())
	}
	cmds = append(cmds, iv.pic.SetSize(iv.cols, iv.rows))
	if iv.placedKey != iv.key {
		iv.placedKey = iv.key
		cmds = append(cmds, iv.pic.SetImage(iv.want))
	}
	return tea.Batch(cmds...)
}

// placed returns the placeholder lines of the image of attachment key once
// the terminal has it, false before that (the encoded frame is on its way)
// and for any other attachment.
func (iv *imageView) placed(key string) ([]string, bool) {
	if key == "" || key != iv.placedKey || iv.pic.Mode() != picture.PictureKitty || !iv.decide().Image {
		return nil, false
	}
	v := iv.pic.View().Content
	if !strings.ContainsRune(v, kitty.Placeholder) {
		// Not placed yet: the picture would draw half-blocks meanwhile, the
		// text image is the better interim.
		return nil, false
	}
	return strings.Split(v, "\n"), true
}

// decode returns the image of the data of attachment key, decoded once.
func (iv *imageView) decode(key string, decode func() (image.Image, error)) (image.Image, error) {
	if iv.decodedKey == key && iv.decoded != nil {
		return iv.decoded, nil
	}
	img, err := decode()
	if err != nil {
		return nil, err
	}
	iv.decodedKey, iv.decoded = key, img
	return img, nil
}

// cellsFor is the size in cells an image is drawn at in a box of cols×rows:
// as large as fits without distortion (see imgtext.Fit).
func cellsFor(img image.Image, cols, rows int) (int, int) {
	b := img.Bounds()
	return imgtext.Fit(b.Dx(), b.Dy(), cols, rows)
}
