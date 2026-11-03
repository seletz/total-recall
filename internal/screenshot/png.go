package screenshot

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"sync"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Geometry of the images in pixels. The font is drawn at twice a typical
// terminal size, so the images stay sharp on high-resolution displays.
const (
	fontSize = 26
	cellW    = 16 // advance of Go Mono at fontSize, rounded
	cellH    = 34
	padX     = 36
	padY     = 32
	titleBar = 60 // room above the first row, holds the window buttons
	radius   = 22
	stroke   = 3 // thickness of box-drawing lines
)

// Window colours: a dark background close to the theme's rules, the
// default text colour of a dark terminal, a border in the rule colour.
var (
	background = color.RGBA{0x14, 0x19, 0x22, 0xff}
	foreground = color.RGBA{0xe4, 0xe8, 0xee, 0xff}
	border     = color.RGBA{0x3a, 0x5b, 0x82, 0xff}
	buttons    = []color.RGBA{{0xff, 0x5f, 0x57, 0xff}, {0xfe, 0xbc, 0x2e, 0xff}, {0x28, 0xc8, 0x40, 0xff}}
)

// faces are the regular and bold Go Mono faces, parsed once.
var faces = sync.OnceValues(func() ([2]font.Face, error) {
	var out [2]font.Face
	for i, ttf := range [][]byte{gomono.TTF, gomonobold.TTF} {
		f, err := opentype.Parse(ttf)
		if err != nil {
			return out, fmt.Errorf("png: font: %w", err)
		}
		out[i], err = opentype.NewFace(f, &opentype.FaceOptions{Size: fontSize, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return out, fmt.Errorf("png: font: %w", err)
		}
	}
	return out, nil
})

// PNG writes the image of a frame; see Image.
func PNG(w io.Writer, f Frame) error {
	img, err := Image(f)
	if err != nil {
		return err
	}
	return png.Encode(w, img)
}

// Image draws a frame as a terminal window: a rounded dark panel with three
// window buttons, one cell per terminal cell. Text is set in Go Mono (bold
// for bold cells, blended towards the background for faint ones, with a
// line for underlined ones; reverse swaps the colours). Block elements,
// shades and the box-drawing glyphs of the views are drawn as shapes, so
// bars, heatmaps and rules are exact and need no glyphs of the font.
func Image(f Frame) (*image.RGBA, error) {
	fs, err := faces()
	if err != nil {
		return nil, err
	}
	cols, rows := f.Shot.Width, f.Shot.Height
	buf := uv.NewScreenBuffer(cols, rows)
	uv.NewStyledString(f.Content).Draw(buf, buf.Bounds())

	w, h := cols*cellW+2*padX, rows*cellH+titleBar+padY
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRounded(img, img.Bounds(), radius, border)
	fillRounded(img, image.Rect(2, 2, w-2, h-2), radius-2, background)
	for i, c := range buttons {
		fillCircle(img, padX+8+i*26, titleBar/2, 8, c)
	}

	metrics := fs[0].Metrics()
	ascent := (cellH-(metrics.Ascent+metrics.Descent).Ceil())/2 + metrics.Ascent.Ceil()
	for y := range rows {
		for x := range cols {
			cell := buf.CellAt(x, y)
			if cell == nil || cell.Width == 0 || cell.Content == "" {
				continue
			}
			fg, bg := colours(cell.Style)
			r := image.Rect(padX+x*cellW, titleBar+y*cellH, padX+(x+cell.Width)*cellW, titleBar+(y+1)*cellH)
			if bg != nil {
				fill(img, r, bg)
			}
			if fg == nil {
				fg = foreground
			}
			if cell.Style.Attrs&uv.AttrFaint != 0 {
				fg = blend(fg, bgOr(bg), 0.55)
			}
			ch, _ := utf8.DecodeRuneInString(cell.Content)
			if !shape(img, r, ch, fg) {
				face := fs[0]
				if cell.Style.Attrs&uv.AttrBold != 0 {
					face = fs[1]
				}
				d := font.Drawer{Dst: img, Src: image.NewUniform(fg), Face: face,
					Dot: fixed.P(r.Min.X, r.Min.Y+ascent)}
				d.DrawString(cell.Content)
			}
			if cell.Style.Underline != uv.UnderlineNone {
				fill(img, image.Rect(r.Min.X, r.Min.Y+ascent+4, r.Max.X, r.Min.Y+ascent+6), fg)
			}
		}
	}
	return img, nil
}

// colours resolves the foreground and background of a cell: nil means the
// default; reverse swaps them.
func colours(s uv.Style) (fg, bg color.Color) {
	fg, bg = s.Fg, s.Bg
	if s.Attrs&uv.AttrReverse != 0 {
		if fg == nil {
			fg = foreground
		}
		if bg == nil {
			bg = background
		}
		fg, bg = bg, fg
	}
	return fg, bg
}

func bgOr(bg color.Color) color.Color {
	if bg == nil {
		return background
	}
	return bg
}

// shape draws the glyphs the views build charts and rules from and reports
// whether r was one of them.
func shape(img *image.RGBA, r image.Rectangle, ch rune, fg color.Color) bool {
	w, h := r.Dx(), r.Dy()
	cx, cy := r.Min.X+w/2, r.Min.Y+h/2
	rad := min(w, h) / 2 // radius of the rounded corners
	hline := func(x0, x1 int) { fill(img, image.Rect(x0, cy-stroke/2, x1, cy-stroke/2+stroke), fg) }
	vline := func(y0, y1 int) { fill(img, image.Rect(cx-stroke/2, y0, cx-stroke/2+stroke, y1), fg) }
	switch ch {
	case '█':
		fill(img, r, fg)
	case '░', '▒', '▓':
		fill(img, r, alpha(fg, [...]float64{0.3, 0.55, 0.8}[ch-'░']))
	case '▁', '▂', '▃', '▄', '▅', '▆', '▇':
		k := int(ch-'▁') + 1
		fill(img, image.Rect(r.Min.X, r.Max.Y-h*k/8, r.Max.X, r.Max.Y), fg)
	case '▀':
		fill(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, cy), fg)
	case '▉', '▊', '▋', '▌', '▍', '▎', '▏':
		k := 8 - int(ch-'█')
		fill(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+w*k/8, r.Max.Y), fg)
	case '▐':
		fill(img, image.Rect(cx, r.Min.Y, r.Max.X, r.Max.Y), fg)
	case '─':
		hline(r.Min.X, r.Max.X)
	case '│':
		vline(r.Min.Y, r.Max.Y)
	case '┼':
		hline(r.Min.X, r.Max.X)
		vline(r.Min.Y, r.Max.Y)
	case '┬':
		hline(r.Min.X, r.Max.X)
		vline(cy, r.Max.Y)
	case '┴':
		hline(r.Min.X, r.Max.X)
		vline(r.Min.Y, cy)
	case '├':
		vline(r.Min.Y, r.Max.Y)
		hline(cx, r.Max.X)
	case '┤':
		vline(r.Min.Y, r.Max.Y)
		hline(r.Min.X, cx)
	case '╵':
		vline(r.Min.Y, cy+stroke/2)
	case '╷':
		vline(cy-stroke/2, r.Max.Y)
	case '╴':
		hline(r.Min.X, cx+stroke/2)
	case '╶':
		hline(cx-stroke/2, r.Max.X)
	case '┌':
		hline(cx, r.Max.X)
		vline(cy, r.Max.Y)
	case '┐':
		hline(r.Min.X, cx)
		vline(cy, r.Max.Y)
	case '└':
		hline(cx, r.Max.X)
		vline(r.Min.Y, cy)
	case '┘':
		hline(r.Min.X, cx)
		vline(r.Min.Y, cy)
	case '╭':
		arc(img, cx+rad, cy+rad, rad, -1, -1, fg)
		hline(cx+rad, r.Max.X)
		vline(cy+rad, r.Max.Y)
	case '╮':
		arc(img, cx-rad, cy+rad, rad, 1, -1, fg)
		hline(r.Min.X, cx-rad)
		vline(cy+rad, r.Max.Y)
	case '╰':
		arc(img, cx+rad, cy-rad, rad, -1, 1, fg)
		hline(cx+rad, r.Max.X)
		vline(r.Min.Y, cy-rad)
	case '╯':
		arc(img, cx-rad, cy-rad, rad, 1, 1, fg)
		hline(r.Min.X, cx-rad)
		vline(r.Min.Y, cy-rad)
	case '▸', '▶':
		triangle(img, r, fg, true)
	case '◄', '◀':
		triangle(img, r, fg, false)
	default:
		return false
	}
	return true
}

// arc draws the quarter of a circle of radius rad around (ox, oy) that
// lies towards sx, sy (each -1 or +1), as a stroke of the usual thickness
// with soft edges.
func arc(img *image.RGBA, ox, oy, rad, sx, sy int, c color.Color) {
	for y := oy - rad - stroke; y <= oy+rad+stroke; y++ {
		for x := ox - rad - stroke; x <= ox+rad+stroke; x++ {
			dx, dy := float64(x)+0.5-float64(ox), float64(y)+0.5-float64(oy)
			if dx*float64(sx) < 0 || dy*float64(sy) < 0 {
				continue
			}
			d := math.Abs(math.Hypot(dx, dy) - float64(rad))
			if t := float64(stroke)/2 + 0.5 - d; t > 0 {
				img.Set(x, y, blend(c, img.RGBAAt(x, y), math.Min(t, 1)))
			}
		}
	}
}

// triangle draws a right- or left-pointing triangle in the middle of r.
func triangle(img *image.RGBA, r image.Rectangle, c color.Color, right bool) {
	w, h := r.Dx(), r.Dy()
	top, bottom := r.Min.Y+h/4, r.Max.Y-h/4
	left, width := r.Min.X+w/5, w*3/5
	for y := top; y < bottom; y++ {
		// Width of the triangle at this row: full at the base, zero at the tip.
		t := 1 - math.Abs(float64(2*(y-top))/float64(bottom-top)-1)
		span := int(math.Round(t * float64(width)))
		if right {
			fill(img, image.Rect(left, y, left+span, y+1), c)
		} else {
			fill(img, image.Rect(left+width-span, y, left+width, y+1), c)
		}
	}
}

func fill(img *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Over)
}

// fillRounded fills r with c, leaving the corners outside a radius empty.
func fillRounded(img *image.RGBA, r image.Rectangle, rad int, c color.Color) {
	mask := image.NewAlpha(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			mask.SetAlpha(x, y, color.Alpha{A: uint8(255 * coverage(x, y, r, rad))})
		}
	}
	draw.DrawMask(img, r, image.NewUniform(c), image.Point{}, mask, r.Min, draw.Over)
}

// coverage is how much of the pixel at x, y lies inside the rounded
// rectangle r: 1 away from the corners, falling off over one pixel at the
// corner arcs.
func coverage(x, y int, r image.Rectangle, rad int) float64 {
	cx := math.Min(math.Max(float64(x)+0.5, float64(r.Min.X+rad)), float64(r.Max.X-rad))
	cy := math.Min(math.Max(float64(y)+0.5, float64(r.Min.Y+rad)), float64(r.Max.Y-rad))
	d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
	return math.Min(math.Max(float64(rad)+0.5-d, 0), 1)
}

// fillCircle draws a filled circle of radius rad around x, y.
func fillCircle(img *image.RGBA, x, y, rad int, c color.Color) {
	r := image.Rect(x-rad, y-rad, x+rad, y+rad)
	mask := image.NewAlpha(r)
	for py := r.Min.Y; py < r.Max.Y; py++ {
		for px := r.Min.X; px < r.Max.X; px++ {
			d := math.Hypot(float64(px)+0.5-float64(x), float64(py)+0.5-float64(y))
			mask.SetAlpha(px, py, color.Alpha{A: uint8(255 * math.Min(math.Max(float64(rad)+0.5-d, 0), 1))})
		}
	}
	draw.DrawMask(img, r, image.NewUniform(c), image.Point{}, mask, r.Min, draw.Over)
}

// blend mixes a over b: t = 1 is a, t = 0 is b.
func blend(a, b color.Color, t float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	mix := func(x, y uint32) uint8 { return uint8((float64(x>>8)*t + float64(y>>8)*(1-t)) + 0.5) }
	return color.RGBA{mix(ar, br), mix(ag, bg), mix(ab, bb), 0xff}
}

// alpha returns c with the given opacity.
func alpha(c color.Color, a float64) color.Color {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(255*a + 0.5)}
}
