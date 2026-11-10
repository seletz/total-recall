// Package imgtext draws a raster image as text, for terminals without a
// graphics protocol (and for the screenshots and goldens): one cell per
// block of pixels, a shade glyph for its brightness, in the colour of the
// block as foreground. The glyph alone carries the picture, so it stays
// readable with NO_COLOR, where the renderer drops the colours, and the
// shades are chosen for the background of the terminal (dense = bright on
// a dark one, dense = dark on a light one). Render is a pure function.
package imgtext

import (
	"fmt"
	"image"
	"strings"
)

// shades go from empty to full.
var shades = []rune{' ', '░', '▒', '▓', '█'}

// cellAspect is the height of a terminal cell over its width.
const cellAspect = 2

// Fit returns the size in cells an image of iw×ih pixels is drawn at in a
// box of w×h cells: as large as fits without distorting it (a cell is
// twice as high as wide) and never larger than the image itself, which
// has one cell per pixel column and per two pixel rows at most. Zero in,
// zero out.
func Fit(iw, ih, w, h int) (cw, ch int) {
	if iw <= 0 || ih <= 0 || w <= 0 || h <= 0 {
		return 0, 0
	}
	cw = min(w, iw)
	ch = max((ih*cw+iw*cellAspect/2)/(iw*cellAspect), 1) // round
	if ch > h {
		ch = h
		cw = max(min(iw*ch*cellAspect/ih, w), 1)
	}
	if ch > max(ih/cellAspect, 1) {
		ch = max(ih/cellAspect, 1)
	}
	return cw, ch
}

// Render draws img in a box of w×h cells and returns one string per row,
// each at most w cells wide. A transparent pixel counts as background. It
// returns nil for a nil or empty image or a box without room.
func Render(img image.Image, w, h int, dark bool) []string {
	if img == nil {
		return nil
	}
	b := img.Bounds()
	cw, ch := Fit(b.Dx(), b.Dy(), w, h)
	if cw == 0 || ch == 0 {
		return nil
	}
	lines := make([]string, ch)
	var sb strings.Builder
	for cy := 0; cy < ch; cy++ {
		sb.Reset()
		y0, y1 := b.Min.Y+cy*b.Dy()/ch, b.Min.Y+(cy+1)*b.Dy()/ch
		for cx := 0; cx < cw; cx++ {
			x0, x1 := b.Min.X+cx*b.Dx()/cw, b.Min.X+(cx+1)*b.Dx()/cw
			r, g, bl, a := average(img, x0, x1, y0, y1)
			glyph := shade(r, g, bl, a, dark)
			if glyph == ' ' {
				sb.WriteByte(' ')
				continue
			}
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm%c\x1b[m", r, g, bl, glyph)
		}
		lines[cy] = sb.String()
	}
	return lines
}

// average is the mean colour of the pixels [x0,x1)×[y0,y1), weighted by
// alpha; a is the mean alpha. r, g, b are 0–255.
func average(img image.Image, x0, x1, y0, y1 int) (r, g, b, a int) {
	var sr, sg, sb, sa, n uint64
	for y := y0; y < max(y1, y0+1); y++ {
		for x := x0; x < max(x1, x0+1); x++ {
			pr, pg, pb, pa := img.At(x, y).RGBA() // alpha-premultiplied, 16 bit
			sr, sg, sb, sa, n = sr+uint64(pr), sg+uint64(pg), sb+uint64(pb), sa+uint64(pa), n+1
		}
	}
	if n == 0 || sa == 0 {
		return 0, 0, 0, 0
	}
	// Un-premultiply the sums: colour = sum / alpha-sum.
	r, g, b = int(sr*255/sa), int(sg*255/sa), int(sb*255/sa)
	a = int(sa * 255 / (n * 0xffff))
	return r, g, b, a
}

// shade picks the glyph of a cell by its brightness.
func shade(r, g, b, a int, dark bool) rune {
	if a < 32 {
		return ' '
	}
	lum := (2126*r + 7152*g + 722*b) / 10000 // Rec. 709, 0–255
	if !dark {
		lum = 255 - lum
	}
	return shades[min(lum*len(shades)/256, len(shades)-1)]
}
