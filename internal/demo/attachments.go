package demo

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// The attachments of the demo archive: an invented screenshot of an
// elevation profile and an invented one-page document. Both are made in
// code from integers only, so the bytes (and with them the hashes in the
// screenshots) do not depend on the platform.

// profilePoints is the height of the profile in pixels, from the left, as
// (x, y) corners of the line; y counts from the top of a 48-row image.
var profilePoints = [][2]int{{0, 36}, {16, 33}, {30, 24}, {44, 28}, {58, 14}, {72, 20}, {84, 11}, {95, 15}}

const (
	profileW = 96
	profileH = 48
)

// profileImage returns the PNG of the elevation profile: sky, a filled
// area below the profile line and a darker line on top of it.
func profileImage() []byte {
	img := image.NewRGBA(image.Rect(0, 0, profileW, profileH))
	for y := 0; y < profileH; y++ {
		for x := 0; x < profileW; x++ {
			// A sky that gets lighter towards the horizon.
			img.Set(x, y, color.RGBA{R: uint8(30 + y*2), G: uint8(60 + y*3), B: uint8(110 + y*2), A: 255})
		}
	}
	for x := 0; x < profileW; x++ {
		top := profileAt(x)
		for y := top; y < profileH; y++ {
			shade := uint8(min(60+(y-top)*4, 140))
			img.Set(x, y, color.RGBA{R: 40, G: shade + 40, B: 70, A: 255})
		}
		img.Set(x, top, color.RGBA{R: 250, G: 240, B: 200, A: 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// profileAt is the row of the profile line at column x, interpolated
// linearly between the corners.
func profileAt(x int) int {
	for i := 1; i < len(profilePoints); i++ {
		a, b := profilePoints[i-1], profilePoints[i]
		if x <= b[0] {
			return a[1] + (b[1]-a[1])*(x-a[0])/(b[0]-a[0])
		}
	}
	return profilePoints[len(profilePoints)-1][1]
}

// formatNotes returns a small PDF: one page, a title and an author. It is
// made of the file structure alone (no content stream), enough for the
// viewer's metadata.
func formatNotes() []byte {
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	b.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	b.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	b.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] >>\nendobj\n")
	b.WriteString("4 0 obj\n<< /Title (Notes on the track format) /Author (Mara Example) /Producer (kompass docs) >>\nendobj\n")
	b.WriteString("trailer\n<< /Root 1 0 R /Info 4 0 R /Size 5 >>\n%%EOF\n")
	return []byte(b.String())
}
