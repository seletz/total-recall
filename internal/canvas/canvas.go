// Package canvas is a small renderer-neutral drawing surface for the
// charts of `total-recall chart`: a Scene of rectangles, lines, circles and
// text in CSS pixels, written as SVG (SVG) or rasterized as PNG (PNG, pure
// Go). Both outputs come from the same Scene, so a chart looks alike in
// either format and the SVG text is stable for golden tests.
package canvas

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
)

// Scene is a drawing of Width × Height CSS pixels. A Background with alpha
// 0 is transparent; the items are drawn in order.
type Scene struct {
	Width, Height float64
	Background    color.NRGBA
	Items         []Item
}

// Add appends items to the scene.
func (s *Scene) Add(items ...Item) { s.Items = append(s.Items, items...) }

// Item is one drawable: Rect, Line, Circle or Text.
type Item interface {
	item()
}

// Rect is a filled rectangle with optional rounded corners. Title is the
// tooltip of the SVG (<title>); the PNG has none.
type Rect struct {
	X, Y, W, H float64
	Radius     float64
	Fill       color.NRGBA
	Title      string
}

// Line is a straight stroke of the given width.
type Line struct {
	X1, Y1, X2, Y2 float64
	Width          float64
	Stroke         color.NRGBA
}

// Circle is a filled circle; Title as for Rect.
type Circle struct {
	CX, CY, R float64
	Fill      color.NRGBA
	Title     string
}

// Anchor says where the X of a Text lies: at its start, middle or end.
type Anchor int

// Anchors of Text.
const (
	Start Anchor = iota
	Middle
	End
)

// Text is one line of text at the baseline Y, in a system sans-serif
// (SVG) or Go Sans (PNG) of Size pixels.
type Text struct {
	X, Y   float64
	Text   string
	Size   float64
	Bold   bool
	Fill   color.NRGBA
	Anchor Anchor
}

func (Rect) item()   {}
func (Line) item()   {}
func (Circle) item() {}
func (Text) item()   {}

// Transparent is the colour of nothing: a Background that lets the page
// show through.
var Transparent = color.NRGBA{}

// Hex parses "#rrggbb" into an opaque colour. It panics on any other
// input, so it is for literals; configured colours go through
// project.ParseColor first.
func Hex(s string) color.NRGBA {
	if len(s) != 7 || s[0] != '#' {
		panic(fmt.Sprintf("canvas: bad colour %q", s))
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		panic(fmt.Sprintf("canvas: bad colour %q", s))
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

// Alpha returns c with the opacity a (0 … 1).
func Alpha(c color.NRGBA, a float64) color.NRGBA {
	c.A = uint8(math.Round(max(0, min(1, a)) * 255))
	return c
}

// From converts any colour to NRGBA.
func From(c color.Color) color.NRGBA {
	if n, ok := c.(color.NRGBA); ok {
		return n
	}
	return color.NRGBAModel.Convert(c).(color.NRGBA)
}

// HexString formats the colour as #rrggbb, without its alpha.
func HexString(c color.NRGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// Size is the output size of a scene. Scale multiplies the scene (0 means
// 1); Width and Height, when set, are the output size in pixels instead: one
// of them scales the scene to fit, both place the scaled scene centred in
// the given box, as an SVG viewer does.
type Size struct {
	Width, Height int
	Scale         float64
}

// layout resolves the output size for a scene of w × h: the pixel size,
// the scale and the offset of the scene in the output.
func (z Size) layout(w, h float64) (outW, outH int, scale, dx, dy float64) {
	scale = z.Scale
	if scale <= 0 {
		scale = 1
	}
	switch {
	case z.Width > 0 && z.Height > 0:
		outW, outH = z.Width, z.Height
		scale = min(float64(outW)/w, float64(outH)/h)
		dx = (float64(outW) - w*scale) / 2
		dy = (float64(outH) - h*scale) / 2
	case z.Width > 0:
		scale = float64(z.Width) / w
		outW, outH = z.Width, int(math.Round(h*scale))
	case z.Height > 0:
		scale = float64(z.Height) / h
		outW, outH = int(math.Round(w*scale)), z.Height
	default:
		outW, outH = int(math.Round(w*scale)), int(math.Round(h*scale))
	}
	return max(outW, 1), max(outH, 1), scale, dx, dy
}
