package viewer

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/store"
)

func testPNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func blob(media string, n int) store.BlobInfo {
	return store.BlobInfo{SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", MediaType: media, Bytes: int64(n), StoredBytes: int64(n)}
}

func TestSizeLabel(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{{0, "0 B"}, {29, "29 B"}, {1024, "1.0 KiB"}, {35000, "34.2 KiB"}, {3 << 20, "3.0 MiB"}}
	for _, tt := range tests {
		if got := SizeLabel(tt.n); got != tt.want {
			t.Errorf("SizeLabel(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestAttachmentImage(t *testing.T) {
	data := testPNG(t, 40, 20, color.White)
	c := Attachment{Info: blob("image/png", len(data)), Data: data, Index: 1, Count: 2}.Render(Env{Theme: env.Theme, Rows: 20}, 60)
	got := plain(c)
	for _, want := range []string{"image/png", "1 of 2", "40×20", "0123456789ab", "total-recall blob 0123456789ab", "██████"} {
		if !strings.Contains(got, want) {
			t.Errorf("image view lacks %q:\n%s", want, got)
		}
	}
	for _, l := range append(c.Head, c.Body...) {
		if w := ansi.StringWidth(l); w > 60 {
			t.Errorf("line wider than 60: %d %q", w, l)
		}
	}
}

func TestAttachmentImageFitsRows(t *testing.T) {
	data := testPNG(t, 100, 100, color.White)
	for _, rows := range []int{8, 12, 30} {
		c := Attachment{Info: blob("image/png", len(data)), Data: data}.Render(Env{Theme: env.Theme, Rows: rows}, 80)
		if n := len(c.Head) + len(c.Body); n > rows {
			t.Errorf("rows=%d: %d lines", rows, n)
		}
	}
}

func TestAttachmentKinds(t *testing.T) {
	pdf := []byte("%PDF-1.4\n<< /Type /Page >>\n<< /Title (Synthetic memo) >>\n")
	tests := []struct {
		name string
		a    Attachment
		want []string
		not  []string
	}{
		{"pdf", Attachment{Info: blob("application/pdf", len(pdf)), Data: pdf}, []string{"application/pdf", "pages", "1", "Synthetic memo", "PDF 1.4", "total-recall blob"}, nil},
		{"text", Attachment{Info: blob("text/plain", 9), Data: []byte("hello\nbye")}, []string{"text/plain", "hello", "bye"}, nil},
		{"text is wrapped", Attachment{Info: blob("text/plain", 40), Data: []byte(strings.Repeat("word ", 8))}, []string{"word word"}, nil},
		{"binary", Attachment{Info: blob("application/octet-stream", 4), Data: []byte{0, 1, 2, 3}}, []string{"no preview", "total-recall blob"}, nil},
		{"not stored", Attachment{Info: blob("image/png", 500)}, []string{"not stored", "store_blobs"}, []string{"██"}},
		{"broken image", Attachment{Info: blob("image/png", 4), Data: []byte("nope")}, []string{"cannot decode", "total-recall blob"}, nil},
		{"unknown pdf", Attachment{Info: blob("application/pdf", 3), Data: []byte("abc")}, []string{"no PDF header"}, nil},
		{"binary text", Attachment{Info: blob("text/plain", 3), Data: []byte{0xff, 0x00, 0x01}}, []string{"not text"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plain(tt.a.Render(Env{Theme: env.Theme}, 40))
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("lacks %q:\n%s", want, got)
				}
			}
			for _, not := range tt.not {
				if strings.Contains(got, not) {
					t.Errorf("has %q:\n%s", not, got)
				}
			}
		})
	}
}

func TestAttachmentHugeImage(t *testing.T) {
	// A header claiming a huge image is refused before it is decoded.
	data := testPNG(t, 4, 4, color.White)
	big := bytes.Clone(data)
	// IHDR width/height at offsets 16 and 20: 100000 × 100000.
	copy(big[16:20], []byte{0, 1, 0x86, 0xa0})
	copy(big[20:24], []byte{0, 1, 0x86, 0xa0})
	binary.BigEndian.PutUint32(big[29:33], crc32.ChecksumIEEE(big[12:29])) // CRC of the IHDR chunk
	got := plain(Attachment{Info: blob("image/png", len(big)), Data: big}.Render(Env{Theme: env.Theme}, 40))
	if !strings.Contains(got, "too large") {
		t.Errorf("huge image: %s", got)
	}
}

func TestAttachmentNarrow(t *testing.T) {
	data := testPNG(t, 40, 20, color.White)
	for _, w := range []int{1, 5, 12, 20} {
		c := Attachment{Info: blob("image/png", len(data)), Data: data, Index: 1, Count: 3}.Render(Env{Theme: env.Theme, Rows: 10}, w)
		for _, l := range append(c.Head, c.Body...) {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("w=%d: line of %d cells: %q", w, got, ansi.Strip(l))
			}
		}
	}
}

func TestAttachmentNote(t *testing.T) {
	tests := []struct {
		media string
		n     int64
		want  string
	}{{"image/png", 35000, "image/png 34.2 KiB"}, {"application/pdf", 12, "application/pdf 12 B"}}
	for _, tt := range tests {
		if got := AttachmentNote(store.EventAttachment{BlobInfo: store.BlobInfo{MediaType: tt.media, Bytes: tt.n}}); got != tt.want {
			t.Errorf("AttachmentNote = %q, want %q", got, tt.want)
		}
	}
}

func TestAttachmentLines(t *testing.T) {
	atts := []store.EventAttachment{
		{BlobInfo: store.BlobInfo{MediaType: "image/png", Bytes: 2048}},
		{BlobInfo: store.BlobInfo{MediaType: "application/pdf", Bytes: 100}},
	}
	got := plain(Content{Body: attachmentLines(env, atts, 60)})
	for _, want := range []string{"▣", "image/png 2.0 KiB", "application/pdf 100 B"} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q: %s", want, got)
		}
	}
	if attachmentLines(env, nil, 60) != nil {
		t.Error("no attachments, no lines")
	}
}

func TestImageRows(t *testing.T) {
	tests := []struct{ total, want int }{{30, 23}, {10, 3}, {7, 1}, {3, 1}, {0, 24}, {-1, 24}}
	for _, tt := range tests {
		if got := ImageRows(tt.total); got != tt.want {
			t.Errorf("ImageRows(%d) = %d, want %d", tt.total, got, tt.want)
		}
	}
}

func TestDecodeImage(t *testing.T) {
	data := testPNG(t, 6, 4, color.White)
	img, format, err := DecodeImage(data)
	if err != nil || format != "png" || img.Bounds().Dx() != 6 || img.Bounds().Dy() != 4 {
		t.Errorf("DecodeImage = %v, %q, %v", img, format, err)
	}
	if _, _, err := DecodeImage([]byte("nope")); err == nil {
		t.Error("garbage decodes")
	}
}

func TestAttachmentPicture(t *testing.T) {
	// A picture drawn by the terminal replaces the text image: the placeholder
	// lines come through as they are, under the caption.
	data := testPNG(t, 40, 20, color.White)
	pic := []string{"\x1b[38;5;47m\U0010eeee̅̅", "\x1b[38;5;47m\U0010eeee̅̍"}
	c := Attachment{Info: blob("image/png", len(data)), Data: data, Picture: pic}.Render(Env{Theme: env.Theme, Rows: 20}, 30)
	body := strings.Join(c.Body, "\n")
	if !strings.Contains(body, "png 40×20") || !strings.Contains(body, pic[0]) || !strings.Contains(body, pic[1]) {
		t.Errorf("body = %q", body)
	}
	if strings.ContainsAny(body, "░▒▓█") {
		t.Errorf("text image next to the picture: %q", body)
	}
	// Without a data (not stored) the picture is not shown.
	c = Attachment{Info: blob("image/png", 10), Picture: pic}.Render(Env{Theme: env.Theme}, 30)
	if strings.Contains(strings.Join(c.Body, "\n"), pic[0]) {
		t.Error("picture without data")
	}
}
