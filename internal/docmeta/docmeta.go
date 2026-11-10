// Package docmeta tells what an attachment of the archive is and reads
// the little the viewer shows of a document: the metadata of a PDF (a
// scan of its bytes, no PDF library: pages and the title fields a reader
// would show), or the start of a text. It never fails on malformed data,
// it shows less.
package docmeta

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Kind is how the viewer shows an attachment.
type Kind int

// The kinds of attachment.
const (
	// KindBinary is anything else: metadata and the hint for blob.
	KindBinary Kind = iota
	// KindImage is a raster image the viewer decodes (PNG, JPEG, GIF, WebP).
	KindImage
	// KindPDF is a PDF: metadata.
	KindPDF
	// KindText is text: its start.
	KindText
)

// MaxText is how many bytes of a text the viewer shows at most.
const MaxText = 64 << 10

// maxField is how many characters of a PDF field are kept.
const maxField = 120

// KindOf returns the kind of a media type, case-insensitively and without
// parameters.
func KindOf(mediaType string) Kind {
	mt, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(mediaType)), ";")
	mt = strings.TrimSpace(mt)
	switch {
	case mt == "image/png", mt == "image/jpeg", mt == "image/gif", mt == "image/webp":
		return KindImage
	case mt == "application/pdf":
		return KindPDF
	case strings.HasPrefix(mt, "text/"), mt == "application/json", mt == "image/svg+xml":
		return KindText
	}
	return KindBinary
}

// Ext returns the file extension, with the dot, for a media type; ".bin"
// when it is not known. The external viewer picks the program by it.
func Ext(mediaType string) string {
	mt, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(mediaType)), ";")
	switch strings.TrimSpace(mt) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "application/pdf":
		return ".pdf"
	case "application/json":
		return ".json"
	case "text/markdown":
		return ".md"
	case "text/plain":
		return ".txt"
	}
	return ".bin"
}

// Text returns the start of data as text (at most MaxText bytes, cut at a
// character); ok is false for data that is not UTF-8 text.
func Text(data []byte) (string, bool) {
	if len(data) > MaxText {
		data = data[:MaxText]
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
			if len(data) < MaxText-utf8.UTFMax {
				break
			}
		}
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", false
	}
	return string(data), true
}

// PDFInfo is what the viewer shows of a PDF. Pages is 0 when the file
// does not tell (page tree in a compressed object stream).
type PDFInfo struct {
	Version                 string
	Pages                   int
	Title, Author, Producer string
}

var (
	pdfVersion = regexp.MustCompile(`^%PDF-(\d\.\d)`)
	pdfPage    = regexp.MustCompile(`/Type\s*/Page([^s]|$)`)
	pdfCount   = regexp.MustCompile(`/Type\s*/Pages\b[^>]*?/Count\s+(\d+)`)
)

// PDF reads the metadata of a PDF; ok is false for data that does not
// start with the PDF header.
func PDF(data []byte) (info PDFInfo, ok bool) {
	m := pdfVersion.FindSubmatch(data)
	if m == nil {
		return PDFInfo{}, false
	}
	info.Version = string(m[1])
	info.Pages = len(pdfPage.FindAll(data, -1))
	if info.Pages == 0 {
		if c := pdfCount.FindSubmatch(data); c != nil {
			info.Pages, _ = strconv.Atoi(string(c[1]))
		}
	}
	info.Title = pdfString(data, "/Title")
	info.Author = pdfString(data, "/Author")
	info.Producer = pdfString(data, "/Producer")
	return info, true
}

// pdfString returns the literal string after key ("(…)", with \( \) \\
// escapes), cut to maxField characters; "" when there is none (a hex
// string is not decoded).
func pdfString(data []byte, key string) string {
	i := bytes.Index(data, []byte(key))
	if i < 0 {
		return ""
	}
	rest := bytes.TrimLeft(data[i+len(key):], " \t\r\n")
	if len(rest) == 0 || rest[0] != '(' {
		return ""
	}
	var b strings.Builder
	depth := 0
	for j := 0; j < len(rest); j++ {
		c := rest[j]
		switch {
		case c == '\\' && j+1 < len(rest):
			j++
			b.WriteByte(rest[j])
		case c == '(':
			if depth++; depth > 1 {
				b.WriteByte(c)
			}
		case c == ')':
			if depth--; depth == 0 {
				return clip(b.String())
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return "" // unterminated
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxField {
		return s
	}
	return string([]rune(s)[:maxField]) + "…"
}
