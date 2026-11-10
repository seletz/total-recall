package viewer

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // decoders of the media types KindImage names
	_ "image/jpeg" // see docmeta.KindOf
	_ "image/png"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	_ "golang.org/x/image/webp"

	"github.com/seletz/total-recall/internal/docmeta"
	"github.com/seletz/total-recall/internal/imgtext"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/session"
)

// maxPixels is the largest image the viewer decodes (width × height); a
// header that claims more is refused before any pixel is read.
const maxPixels = 40_000_000

// defaultRows is the height an image is fitted to when the caller does
// not tell.
const defaultRows = 24

// AttachmentMark marks an event with attachments in the lists and views.
const AttachmentMark = session.AttachmentMark

// SizeLabel formats a byte count: "29 B", "1.2 KiB", "3.4 MiB".
func SizeLabel(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return strconv.FormatInt(n, 10) + " B"
}

// AttachmentNote is the short description of an attachment: its media
// type and size.
func AttachmentNote(a store.EventAttachment) string {
	return a.MediaType + " " + SizeLabel(a.Bytes)
}

// attachmentLines are the lines a viewer adds under an event with
// attachments: the mark and the note of each, muted, cut to w.
func attachmentLines(env Env, atts []store.EventAttachment, w int) []string {
	if len(atts) == 0 {
		return nil
	}
	out := make([]string, len(atts))
	for i, a := range atts {
		out[i] = env.Theme.Mark.Render(AttachmentMark) + " " + env.Theme.Muted.Render(cut(AttachmentNote(a), max(w-2, 0)))
	}
	return out
}

// Attachment shows one attachment of an event: the media type and size
// in the head, then by kind the image as text (shade glyphs, see package
// imgtext), the metadata of a PDF, the start of a text, or just the
// metadata. Every kind ends with the command that writes the data to a
// file, the fallback for what the viewer cannot show. It is the viewer
// of the attachment view.
type Attachment struct {
	Info store.BlobInfo
	// Data is the attachment, nil when the archive does not hold it
	// (ingest.store_blobs = false).
	Data []byte
	// Index and Count place it among the attachments of its event
	// (1-based); Count 0 shows no position.
	Index, Count int
	// Picture, when set, is the image already drawn by the terminal: the
	// placeholder lines of a Kitty Graphics image (ImageRows tall at most).
	// The viewer shows them under the caption instead of the text image.
	Picture []string
}

// Render implements Viewer. The anchor is -1: an attachment has no hit.
func (v Attachment) Render(env Env, w int) Content {
	c := Content{Anchor: -1}
	second := SizeLabel(v.Info.Bytes)
	if v.Count > 1 {
		second += fmt.Sprintf("  %d of %d", v.Index, v.Count)
	}
	c.Head = []string{
		" " + env.Theme.Title.Render(cut(v.Info.MediaType, w)),
		" " + env.Theme.Muted.Render(cut(second, w)),
		" " + env.Theme.Muted.Render(cut(short(v.Info.SHA256), w)),
		rule(env, w),
	}
	// The body is indented by one cell when there is room for it.
	indent := ""
	if w > 1 {
		indent = " "
	}
	inner := max(w-len(indent), 1)
	for _, l := range v.body(env, inner, v.rows(env, len(c.Head))) {
		c.Body = append(c.Body, indent+l)
	}
	c.Body = append(c.Body, "", indent+env.Theme.Muted.Render(cut("total-recall blob "+short(v.Info.SHA256)+" --out FILE", inner)))
	return c
}

// headRows and tailRows are the lines of an attachment around its body: the
// head (type, size and position, hash, rule) and the blank line with the
// blob command at the end.
const (
	headRows = 4
	tailRows = 2
)

// ImageRows is the height an image can have in a column of total rows: what
// is left under the head, the caption line and the blob command; at least 1.
func ImageRows(total int) int {
	if total <= 0 {
		total = defaultRows + headRows + tailRows + 1
	}
	return max(total-headRows-tailRows-1, 1)
}

// DecodeImage decodes the data of an image: the image, its format name and
// its size. A header that claims more than maxPixels is refused before any
// pixel is read, so a hostile file cannot take the memory.
func DecodeImage(data []byte) (image.Image, string, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxPixels/cfg.Height {
		return nil, "", fmt.Errorf("image too large to show (%d×%d)", cfg.Width, cfg.Height)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	return img, format, err
}

// short is the hash as the CLI shows it: the first 12 characters.
func short(sha string) string { return sha[:min(len(sha), 12)] }

// rows is the height left for the body under the head, without the two
// lines of the blob hint; at least 1.
func (v Attachment) rows(env Env, head int) int {
	total := env.Rows
	if total <= 0 {
		total = defaultRows + head + 2
	}
	return max(total-head-2, 1)
}

// body renders the content in width w and at most rows lines (an image is
// fitted to them; text is cut).
func (v Attachment) body(env Env, w, rows int) []string {
	muted := func(s string) string { return env.Theme.Muted.Render(cut(s, w)) }
	if v.Data == nil {
		return []string{muted("data not stored (ingest.store_blobs = false)")}
	}
	switch docmeta.KindOf(v.Info.MediaType) {
	case docmeta.KindImage:
		return v.imageBody(env, w, rows)
	case docmeta.KindPDF:
		info, ok := docmeta.PDF(v.Data)
		if !ok {
			return []string{muted("no PDF header: the data is not a PDF")}
		}
		out := []string{muted("PDF " + info.Version)}
		if info.Pages > 0 {
			out = append(out, field(env, "pages", strconv.Itoa(info.Pages), w))
		}
		for _, f := range [][2]string{{"title", info.Title}, {"author", info.Author}, {"producer", info.Producer}} {
			if f[1] != "" {
				out = append(out, field(env, f[0], f[1], w))
			}
		}
		return out
	case docmeta.KindText:
		text, ok := docmeta.Text(v.Data)
		if !ok {
			return []string{muted("not text: the data is not valid UTF-8")}
		}
		lines := textLines(env, text, w, lipgloss.NewStyle())
		return lines
	}
	return []string{muted("no preview for this type")}
}

// field renders "name  value" with the name muted.
func field(env Env, name, value string, w int) string {
	const nameWidth = 9
	pad := strings.Repeat(" ", max(nameWidth-len(name), 1))
	return env.Theme.Muted.Render(name+pad) + cut(value, max(w-nameWidth, 1))
}

// imageBody decodes the image and draws it as text in the room it has.
func (v Attachment) imageBody(env Env, w, rows int) []string {
	muted := func(s string) string { return env.Theme.Muted.Render(cut(s, w)) }
	img, format, err := DecodeImage(v.Data)
	switch {
	case err != nil && strings.HasPrefix(err.Error(), "image too large"):
		return []string{muted(err.Error())}
	case err != nil:
		return []string{muted("cannot decode the image: " + err.Error())}
	}
	b := img.Bounds()
	out := []string{muted(fmt.Sprintf("%s %d×%d", format, b.Dx(), b.Dy()))}
	if len(v.Picture) > 0 {
		// Drawn by the terminal: the placeholder cells are not text to cut.
		return append(out, v.Picture...)
	}
	out = append(out, imgtext.Render(img, w, max(rows-1, 1), env.Theme.Dark)...)
	return out
}
