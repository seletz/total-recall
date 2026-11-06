package canvas

import (
	"bytes"
	"fmt"
	"html"
	"image/color"
	"math"
	"strconv"
)

// FontFamily is the font stack of the SVG text: the system sans-serif of
// the viewer.
const FontFamily = "-apple-system, 'Segoe UI', Helvetica, Arial, sans-serif"

// SVG writes the scene as an SVG document. The viewBox is the scene; width
// and height follow size, so a scaled SVG keeps its proportions and a
// viewer letterboxes a box of another ratio.
func SVG(s Scene, size Size) []byte {
	outW, outH, _, _, _ := size.layout(s.Width, s.Height)
	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %s %s" font-family="%s">`+"\n",
		outW, outH, num(s.Width), num(s.Height), FontFamily)
	if s.Background.A > 0 {
		fmt.Fprintf(&b, `<rect width="100%%" height="100%%" %s/>`+"\n", fill(s.Background))
	}
	for _, it := range s.Items {
		switch it := it.(type) {
		case Rect:
			fmt.Fprintf(&b, `<rect x="%s" y="%s" width="%s" height="%s"`, num(it.X), num(it.Y), num(it.W), num(it.H))
			if it.Radius > 0 {
				fmt.Fprintf(&b, ` rx="%s"`, num(it.Radius))
			}
			b.WriteString(" " + fill(it.Fill))
			title(&b, "rect", it.Title)
		case Line:
			w := it.Width
			if w <= 0 {
				w = 1
			}
			fmt.Fprintf(&b, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s" stroke-width="%s"`,
				num(it.X1), num(it.Y1), num(it.X2), num(it.Y2), HexString(it.Stroke), num(w))
			if it.Stroke.A < 0xff {
				fmt.Fprintf(&b, ` stroke-opacity="%s"`, num(float64(it.Stroke.A)/255))
			}
			if it.Dash[0] > 0 {
				fmt.Fprintf(&b, ` stroke-dasharray="%s %s"`, num(it.Dash[0]), num(max(it.Dash[1], 0)))
			}
			b.WriteString("/>\n")
		case Circle:
			fmt.Fprintf(&b, `<circle cx="%s" cy="%s" r="%s" %s`, num(it.CX), num(it.CY), num(it.R), fill(it.Fill))
			title(&b, "circle", it.Title)
		case Text:
			fmt.Fprintf(&b, `<text x="%s" y="%s" font-size="%s"`, num(it.X), num(it.Y), num(it.Size))
			if it.Bold {
				b.WriteString(` font-weight="600"`)
			}
			switch it.Anchor {
			case Middle:
				b.WriteString(` text-anchor="middle"`)
			case End:
				b.WriteString(` text-anchor="end"`)
			}
			fmt.Fprintf(&b, ` %s>%s</text>`+"\n", fill(it.Fill), html.EscapeString(it.Text))
		}
	}
	b.WriteString("</svg>\n")
	return b.Bytes()
}

// title closes the element elem, with a <title> child (the tooltip of
// the viewer) when t is set.
func title(b *bytes.Buffer, elem, t string) {
	if t == "" {
		b.WriteString("/>\n")
		return
	}
	fmt.Fprintf(b, `><title>%s</title></%s>`+"\n", html.EscapeString(t), elem)
}

// fill formats the fill attributes of a colour: fill and, when it is
// translucent, fill-opacity; a fully transparent colour is fill="none".
func fill(c color.NRGBA) string {
	if c.A == 0 {
		return `fill="none"`
	}
	s := fmt.Sprintf(`fill="%s"`, HexString(c))
	if c.A < 0xff {
		s += fmt.Sprintf(` fill-opacity="%s"`, num(float64(c.A)/255))
	}
	return s
}

// num formats a coordinate: at most two decimals, no trailing zeros.
func num(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}
