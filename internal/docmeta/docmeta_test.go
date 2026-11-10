package docmeta

import (
	"strings"
	"testing"
)

const miniPDF = "%PDF-1.4\n" +
	"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
	"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>\nendobj\n" +
	"3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n" +
	"4 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n" +
	"5 0 obj\n<< /Title (Quarterly \\(draft\\) notes) /Author (Ada Example) /Producer (synthetic) >>\nendobj\n" +
	"trailer\n<< /Root 1 0 R /Info 5 0 R >>\n%%EOF\n"

func TestPDF(t *testing.T) {
	tests := []struct {
		name string
		data string
		want PDFInfo
		ok   bool
	}{
		{"mini", miniPDF, PDFInfo{Version: "1.4", Pages: 2, Title: "Quarterly (draft) notes", Author: "Ada Example", Producer: "synthetic"}, true},
		{"no info", "%PDF-1.7\n1 0 obj\n<< /Type /Page >>\nendobj\n", PDFInfo{Version: "1.7", Pages: 1}, true},
		{"count without page objects", "%PDF-1.5\n<< /Type /Pages /Count 7 >>\n", PDFInfo{Version: "1.5", Pages: 7}, true},
		{"not a pdf", "hello", PDFInfo{}, false},
		{"empty", "", PDFInfo{}, false},
		{"title is cut", "%PDF-1.4\n<< /Title (" + strings.Repeat("x", 500) + ") >>\n", PDFInfo{Version: "1.4", Title: strings.Repeat("x", maxField) + "…"}, true},
		{"unterminated string", "%PDF-1.4\n<< /Title (abc", PDFInfo{Version: "1.4"}, true},
		{"hex strings are skipped", "%PDF-1.4\n<< /Title <FEFF0041> >>\n", PDFInfo{Version: "1.4"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := PDF([]byte(tt.data))
			if ok != tt.ok || got != tt.want {
				t.Errorf("PDF() = %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestKind(t *testing.T) {
	tests := []struct {
		media string
		want  Kind
	}{
		{"image/png", KindImage},
		{"image/jpeg", KindImage},
		{"image/gif", KindImage},
		{"image/webp", KindImage},
		{"image/svg+xml", KindText},
		{"application/pdf", KindPDF},
		{"text/plain", KindText},
		{"text/markdown", KindText},
		{"text/plain; charset=utf-8", KindText},
		{"application/json", KindText},
		{"application/octet-stream", KindBinary},
		{"", KindBinary},
		{"IMAGE/PNG", KindImage},
	}
	for _, tt := range tests {
		if got := KindOf(tt.media); got != tt.want {
			t.Errorf("KindOf(%q) = %v, want %v", tt.media, got, tt.want)
		}
	}
}

func TestExt(t *testing.T) {
	tests := []struct{ media, want string }{
		{"image/png", ".png"}, {"image/jpeg", ".jpg"}, {"application/pdf", ".pdf"},
		{"text/plain", ".txt"}, {"text/markdown", ".md"}, {"application/octet-stream", ".bin"}, {"", ".bin"},
		{"image/svg+xml", ".svg"}, {"application/json", ".json"},
	}
	for _, tt := range tests {
		if got := Ext(tt.media); got != tt.want {
			t.Errorf("Ext(%q) = %q, want %q", tt.media, got, tt.want)
		}
	}
}

func TestText(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
		ok   bool
	}{
		{"plain", "a\nb", "a\nb", true},
		{"nul is binary", "a\x00b", "", false},
		{"invalid utf8", "a\xffb", "", false},
		{"cut at the limit", strings.Repeat("x", MaxText+10), strings.Repeat("x", MaxText), true},
		{"cut keeps runes whole", strings.Repeat("x", MaxText-1) + "é", strings.Repeat("x", MaxText-1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Text([]byte(tt.data))
			if ok != tt.ok || got != tt.want {
				t.Errorf("Text() = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
