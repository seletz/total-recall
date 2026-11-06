package canvas

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// kappa is the control-point distance that makes a cubic Bézier follow a
// quarter circle.
const kappa = 0.5522847498

// fonts are Go Sans regular and bold, parsed once.
var fonts = sync.OnceValues(func() ([2]*opentype.Font, error) {
	var out [2]*opentype.Font
	for i, ttf := range [][]byte{goregular.TTF, gobold.TTF} {
		f, err := opentype.Parse(ttf)
		if err != nil {
			return out, fmt.Errorf("canvas: font: %w", err)
		}
		out[i] = f
	}
	return out, nil
})

// faceCache holds one face per size and weight; sizes repeat across the
// texts of a scene.
type faceCache struct {
	mu    sync.Mutex
	faces map[faceKey]font.Face
}

type faceKey struct {
	size float64
	bold bool
}

var faces = &faceCache{faces: map[faceKey]font.Face{}}

func (c *faceCache) get(size float64, bold bool) (font.Face, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := faceKey{size: size, bold: bold}
	if f, ok := c.faces[k]; ok {
		return f, nil
	}
	fs, err := fonts()
	if err != nil {
		return nil, err
	}
	f := fs[0]
	if bold {
		f = fs[1]
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, fmt.Errorf("canvas: font face: %w", err)
	}
	c.faces[k] = face
	return face, nil
}

// PNG rasterizes the scene at size and encodes it as PNG.
func PNG(s Scene, size Size) ([]byte, error) {
	img, err := Image(s, size)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, fmt.Errorf("canvas: encode png: %w", err)
	}
	return b.Bytes(), nil
}

// Image rasterizes the scene at size: shapes anti-aliased through
// x/image/vector, text set in Go Sans. A transparent background leaves
// the pixels outside the items fully transparent.
func Image(s Scene, size Size) (*image.RGBA, error) {
	outW, outH, scale, dx, dy := size.layout(s.Width, s.Height)
	img := image.NewRGBA(image.Rect(0, 0, outW, outH))
	if s.Background.A > 0 {
		draw.Draw(img, img.Bounds(), image.NewUniform(s.Background), image.Point{}, draw.Src)
	}
	r := vector.NewRasterizer(outW, outH)
	r.DrawOp = draw.Over
	tx := func(x float64) float32 { return float32(x*scale + dx) }
	ty := func(y float64) float32 { return float32(y*scale + dy) }
	for _, it := range s.Items {
		switch it := it.(type) {
		case Rect:
			if it.Fill.A == 0 || it.W <= 0 || it.H <= 0 {
				continue
			}
			r.Reset(outW, outH)
			roundedRect(r, tx(it.X), ty(it.Y), float32(it.W*scale), float32(it.H*scale), float32(it.Radius*scale))
			r.Draw(img, img.Bounds(), image.NewUniform(it.Fill), image.Point{})
		case Line:
			if it.Stroke.A == 0 {
				continue
			}
			w := it.Width
			if w <= 0 {
				w = 1
			}
			r.Reset(outW, outH)
			if it.Dash[0] > 0 {
				dashed(r, tx(it.X1), ty(it.Y1), tx(it.X2), ty(it.Y2), float32(w*scale), float32(it.Dash[0]*scale), float32(max(it.Dash[1], 0)*scale))
			} else {
				stroke(r, tx(it.X1), ty(it.Y1), tx(it.X2), ty(it.Y2), float32(w*scale))
			}
			r.Draw(img, img.Bounds(), image.NewUniform(it.Stroke), image.Point{})
		case Circle:
			if it.Fill.A == 0 || it.R <= 0 {
				continue
			}
			r.Reset(outW, outH)
			circle(r, tx(it.CX), ty(it.CY), float32(it.R*scale))
			r.Draw(img, img.Bounds(), image.NewUniform(it.Fill), image.Point{})
		case Text:
			if it.Text == "" || it.Fill.A == 0 {
				continue
			}
			face, err := faces.get(math.Round(it.Size*scale*4)/4, it.Bold)
			if err != nil {
				return nil, err
			}
			d := &font.Drawer{Dst: img, Src: image.NewUniform(it.Fill), Face: face}
			x := float64(tx(it.X))
			if it.Anchor != Start {
				w := float64(d.MeasureString(it.Text)) / 64
				if it.Anchor == Middle {
					w /= 2
				}
				x -= w
			}
			d.Dot = fixed.Point26_6{X: fixed.Int26_6(math.Round(x * 64)), Y: fixed.Int26_6(math.Round(float64(ty(it.Y)) * 64))}
			d.DrawString(it.Text)
		}
	}
	return img, nil
}

// roundedRect adds a rectangle with corner radius rad (clipped to half the
// shorter side) to the path.
func roundedRect(r *vector.Rasterizer, x, y, w, h, rad float32) {
	rad = min(rad, w/2, h/2)
	if rad <= 0 {
		r.MoveTo(x, y)
		r.LineTo(x+w, y)
		r.LineTo(x+w, y+h)
		r.LineTo(x, y+h)
		r.ClosePath()
		return
	}
	c := rad * kappa
	r.MoveTo(x+rad, y)
	r.LineTo(x+w-rad, y)
	r.CubeTo(x+w-rad+c, y, x+w, y+rad-c, x+w, y+rad)
	r.LineTo(x+w, y+h-rad)
	r.CubeTo(x+w, y+h-rad+c, x+w-rad+c, y+h, x+w-rad, y+h)
	r.LineTo(x+rad, y+h)
	r.CubeTo(x+rad-c, y+h, x, y+h-rad+c, x, y+h-rad)
	r.LineTo(x, y+rad)
	r.CubeTo(x, y+rad-c, x+rad-c, y, x+rad, y)
	r.ClosePath()
}

// circle adds a circle of four Bézier arcs to the path.
func circle(r *vector.Rasterizer, cx, cy, rad float32) {
	c := rad * kappa
	r.MoveTo(cx+rad, cy)
	r.CubeTo(cx+rad, cy+c, cx+c, cy+rad, cx, cy+rad)
	r.CubeTo(cx-c, cy+rad, cx-rad, cy+c, cx-rad, cy)
	r.CubeTo(cx-rad, cy-c, cx-c, cy-rad, cx, cy-rad)
	r.CubeTo(cx+c, cy-rad, cx+rad, cy-c, cx+rad, cy)
	r.ClosePath()
}

// dashed adds a line of width w as dashes of length on, separated by gaps
// of length off.
func dashed(r *vector.Rasterizer, x1, y1, x2, y2, w, on, off float32) {
	dx, dy := x2-x1, y2-y1
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l == 0 {
		return
	}
	ux, uy := dx/l, dy/l
	for p := float32(0); p < l; p += on + off {
		q := min(p+on, l)
		stroke(r, x1+ux*p, y1+uy*p, x1+ux*q, y1+uy*q, w)
	}
}

// stroke adds a line of width w as a quadrilateral to the path.
func stroke(r *vector.Rasterizer, x1, y1, x2, y2, w float32) {
	dx, dy := x2-x1, y2-y1
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l == 0 {
		return
	}
	nx, ny := -dy/l*w/2, dx/l*w/2
	r.MoveTo(x1+nx, y1+ny)
	r.LineTo(x2+nx, y2+ny)
	r.LineTo(x2-nx, y2-ny)
	r.LineTo(x1-nx, y1-ny)
	r.ClosePath()
}

// At returns the colour of a pixel of img as NRGBA, for tests and checks.
func At(img *image.RGBA, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}
