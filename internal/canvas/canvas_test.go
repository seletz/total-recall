package canvas

import (
	"image/color"
	"strings"
	"testing"
)

func TestHex(t *testing.T) {
	if got, want := Hex("#4e79a7"), (color.NRGBA{0x4e, 0x79, 0xa7, 0xff}); got != want {
		t.Errorf("Hex = %v, want %v", got, want)
	}
	if got := HexString(Alpha(Hex("#4e79a7"), 0.5)); got != "#4e79a7" {
		t.Errorf("HexString = %q", got)
	}
	if got := Alpha(Hex("#000000"), 0.45).A; got != 115 {
		t.Errorf("Alpha(0.45).A = %d, want 115", got)
	}
	if got := Alpha(Hex("#000000"), 2).A; got != 255 {
		t.Errorf("Alpha(2).A = %d, want 255", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("Hex(\"red\") did not panic")
		}
	}()
	Hex("red")
}

func TestLayout(t *testing.T) {
	tests := []struct {
		name   string
		size   Size
		w, h   int
		scale  float64
		dx, dy float64
	}{
		{"natural", Size{}, 360, 320, 1, 0, 0},
		{"scale", Size{Scale: 2}, 720, 640, 2, 0, 0},
		{"width", Size{Width: 720}, 720, 640, 2, 0, 0},
		{"height", Size{Height: 160}, 180, 160, 0.5, 0, 0},
		{"box wider", Size{Width: 800, Height: 320}, 800, 320, 1, 220, 0},
		{"box taller", Size{Width: 360, Height: 400}, 360, 400, 1, 0, 40},
		{"width wins over scale", Size{Width: 180, Scale: 3}, 180, 160, 0.5, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h, scale, dx, dy := tt.size.layout(360, 320)
			if w != tt.w || h != tt.h || scale != tt.scale || dx != tt.dx || dy != tt.dy {
				t.Errorf("layout = %d×%d scale %g offset %g,%g; want %d×%d scale %g offset %g,%g",
					w, h, scale, dx, dy, tt.w, tt.h, tt.scale, tt.dx, tt.dy)
			}
		})
	}
}

// sample is a scene with one item of every kind.
func sample(bg color.NRGBA) Scene {
	s := Scene{Width: 100, Height: 50, Background: bg}
	s.Add(
		Rect{X: 0, Y: 0, W: 10, H: 10, Fill: Hex("#e15759")},
		Rect{X: 20, Y: 0, W: 10.5, H: 10.25, Radius: 2, Fill: Alpha(Hex("#4e79a7"), 0.45), Title: `a <b> & "c"`},
		Line{X1: 0, Y1: 30, X2: 100, Y2: 30, Stroke: Hex("#cccccc")},
		Line{X1: 0, Y1: 40, X2: 100, Y2: 40, Width: 2, Stroke: Alpha(Hex("#000000"), 0.5)},
		Circle{CX: 50, CY: 25, R: 5, Fill: Hex("#59a14f"), Title: "five"},
		Text{X: 50, Y: 45, Text: "Tom & Jerry", Size: 11, Fill: Hex("#8b8b8b"), Anchor: Middle},
		Text{X: 100, Y: 12, Text: "end", Size: 14, Bold: true, Fill: Hex("#333333"), Anchor: End},
	)
	return s
}

func TestSVG(t *testing.T) {
	got := string(SVG(sample(Hex("#ffffff")), Size{}))
	want := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="50" viewBox="0 0 100 50" font-family="-apple-system, 'Segoe UI', Helvetica, Arial, sans-serif">
<rect width="100%" height="100%" fill="#ffffff"/>
<rect x="0" y="0" width="10" height="10" fill="#e15759"/>
<rect x="20" y="0" width="10.5" height="10.25" rx="2" fill="#4e79a7" fill-opacity="0.45"><title>a &lt;b&gt; &amp; &#34;c&#34;</title></rect>
<line x1="0" y1="30" x2="100" y2="30" stroke="#cccccc" stroke-width="1"/>
<line x1="0" y1="40" x2="100" y2="40" stroke="#000000" stroke-width="2" stroke-opacity="0.5"/>
<circle cx="50" cy="25" r="5" fill="#59a14f"><title>five</title></circle>
<text x="50" y="45" font-size="11" text-anchor="middle" fill="#8b8b8b">Tom &amp; Jerry</text>
<text x="100" y="12" font-size="14" font-weight="600" text-anchor="end" fill="#333333">end</text>
</svg>
`
	if got != want {
		t.Errorf("SVG mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestSVGTransparentAndSized(t *testing.T) {
	got := string(SVG(sample(Transparent), Size{Width: 200}))
	if !strings.HasPrefix(got, `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100" viewBox="0 0 100 50"`) {
		t.Errorf("header = %q", strings.SplitN(got, "\n", 2)[0])
	}
	if strings.Contains(got, `width="100%"`) {
		t.Error("transparent scene has a background rect")
	}
}

func TestImage(t *testing.T) {
	img, err := Image(sample(Hex("#ffffff")), Size{Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 200 || b.Dy() != 100 {
		t.Fatalf("size = %v", b)
	}
	tests := []struct {
		name string
		x, y int
		want color.NRGBA
	}{
		{"red rect", 10, 10, Hex("#e15759")},
		{"background", 100, 10, Hex("#ffffff")},
		{"grey line", 150, 60, Hex("#cccccc")},
		{"circle", 100, 50, Hex("#59a14f")},
		{"translucent rect over white", 50, 10, color.NRGBA{0xaf, 0xc3, 0xd8, 0xff}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := At(img, tt.x, tt.y); got != tt.want {
				t.Errorf("pixel %d,%d = %v, want %v", tt.x, tt.y, got, tt.want)
			}
		})
	}
	// The texts leave dark pixels around their baselines.
	if !anyInk(img, 60, 70, 140, 92, Hex("#ffffff")) {
		t.Error("no ink for the middle-anchored text")
	}
	if !anyInk(img, 150, 0, 200, 26, Hex("#ffffff")) {
		t.Error("no ink for the end-anchored text")
	}
	if anyInk(img, 0, 0, 140, 26, Hex("#ffffff")) && !anyInk(img, 0, 0, 22, 22, Hex("#ffffff")) {
		t.Error("ink left of the end-anchored text")
	}
}

func TestImageTransparent(t *testing.T) {
	img, err := Image(sample(Transparent), Size{})
	if err != nil {
		t.Fatal(err)
	}
	if got := At(img, 50, 5); got.A != 0 {
		t.Errorf("background pixel = %v, want transparent", got)
	}
	if got := At(img, 5, 5); got != Hex("#e15759") {
		t.Errorf("rect pixel = %v", got)
	}
}

func TestPNG(t *testing.T) {
	b, err := PNG(sample(Hex("#ffffff")), Size{Width: 50, Height: 50})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "\x89PNG") {
		t.Errorf("not a PNG: %q", b[:8])
	}
}

// anyInk reports whether any pixel of the box differs from bg.
func anyInk(img interface {
	At(x, y int) color.Color
}, x0, y0, x1, y1 int, bg color.NRGBA) bool {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA) != bg {
				return true
			}
		}
	}
	return false
}
