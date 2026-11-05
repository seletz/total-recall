package store

import (
	"bytes"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/model"
)

// Synthetic attachments: a few bytes with a PNG signature, a repetitive
// "PDF" that zstd shrinks.
var (
	testPNG = []byte("\x89PNG\r\n\x1a\nsmall synthetic picture")
	testPDF = []byte("%PDF-1.4\n" + strings.Repeat("1 0 obj << /Type /Page >> endobj\n", 40))
)

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// attachedEvents are two events of one session: e1 has the picture twice
// in its line (tool_result block and toolUseResult, like the Read tool),
// e2 has the picture and the document.
func attachedEvents() ([]model.Session, []model.Event) {
	sess := []model.Session{{ID: "claude:s1", Harness: "claude", NativeID: "s1", Cwd: "/w/app"}}
	line1 := `{"type":"user","uuid":"e1","message":{"content":[{"type":"tool_result","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + b64(testPNG) + `"}}]}]},"toolUseResult":{"file":{"base64":"` + b64(testPNG) + `","type":"image/png"}}}`
	line2 := `{"type":"user","uuid":"e2","message":{"content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + b64(testPNG) + `"}},{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` + b64(testPDF) + `"}}]}}`
	e1 := ev("claude:e1", "claude:s1", "2026-10-31T22:00:00Z", model.ActorTool, model.KindToolResult, "tool", "[image image/png 32 B]")
	e2 := ev("claude:e2", "claude:s1", "2026-10-31T22:01:00Z", model.ActorHuman, model.KindMessage, "user", "[image image/png 32 B]\n[document application/pdf 1.3 KiB]")
	e1.Raw = strings.ReplaceAll(line1, b64(testPNG), blobref.Ref{SHA256: blobref.Hash(testPNG)}.String())
	e2.Raw = strings.ReplaceAll(strings.ReplaceAll(line2, b64(testPNG), blobref.Ref{SHA256: blobref.Hash(testPNG)}.String()), b64(testPDF), blobref.Ref{SHA256: blobref.Hash(testPDF)}.String())
	png := model.Attachment{SHA256: blobref.Hash(testPNG), MediaType: "image/png", Data: testPNG}
	pdf := model.Attachment{SHA256: blobref.Hash(testPDF), MediaType: "application/pdf", Data: testPDF}
	e1.Attachments = []model.Attachment{png, png}
	e2.Attachments = []model.Attachment{png, pdf}
	return sess, []model.Event{e1, e2}
}

func TestWriteAttachments(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := attachedEvents()
	res, err := s.WriteBatchOpts(ctx, SourceState{}, sess, evs, WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 2 || res.Blobs != 2 || res.BlobBytes != int64(len(testPNG)+len(testPDF)) {
		t.Errorf("result = %+v", res)
	}
	// One blob per hash, as delivered for PNG, zstd for the document.
	for q, want := range map[string]int{
		"SELECT count(*) FROM blobs": 2,
		"SELECT count(*) FROM blobs WHERE media_type = 'image/png' AND encoding = '' AND bytes = " + itoa(len(testPNG)) + " AND length(data) = bytes":           1,
		"SELECT count(*) FROM blobs WHERE media_type = 'application/pdf' AND encoding = 'zstd' AND bytes = " + itoa(len(testPDF)) + " AND length(data) < bytes": 1,
		"SELECT count(*) FROM event_blobs": 4,
		"SELECT count(*) FROM event_blobs WHERE event_id = 'claude:e1' AND sha256 = '" + blobref.Hash(testPNG) + "'":                  2,
		"SELECT count(*) FROM event_blobs WHERE event_id = 'claude:e2' AND position = 1 AND sha256 = '" + blobref.Hash(testPDF) + "'": 1,
	} {
		if got := countRows(t, s, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	// Writing the same events again changes nothing.
	res, err = s.WriteBatchOpts(ctx, SourceState{}, sess, evs, WriteOptions{})
	if err != nil || res.Inserted != 0 || res.Blobs != 0 || countRows(t, s, "SELECT count(*) FROM event_blobs") != 4 {
		t.Errorf("second write: %+v, %v", res, err)
	}

	// Blob by full hash and by unique prefix, decoded.
	for _, ref := range []string{blobref.Hash(testPDF), blobref.Hash(testPDF)[:12]} {
		info, data, err := s.Blob(ctx, ref)
		if err != nil || info.SHA256 != blobref.Hash(testPDF) || info.MediaType != "application/pdf" || info.Bytes != int64(len(testPDF)) || !bytes.Equal(data, testPDF) {
			t.Errorf("Blob(%s) = %+v, %d bytes, %v", ref, info, len(data), err)
		}
	}
	info, data, err := s.Blob(ctx, blobref.Hash(testPNG))
	if err != nil || info.MediaType != "image/png" || !bytes.Equal(data, testPNG) {
		t.Errorf("Blob(png) = %+v, %v", info, err)
	}
	if _, _, err := s.Blob(ctx, "0000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Blob(unknown) = %v", err)
	}
	if _, _, err := s.Blob(ctx, ""); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("Blob(\"\") = %v, want ErrAmbiguous", err)
	}
	if _, _, err := s.Blob(ctx, "zz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Blob(zz) = %v", err)
	}
	// BlobEvents lists the events that reference a blob, in order.
	if ids, err := s.BlobEvents(ctx, blobref.Hash(testPNG)); err != nil || strings.Join(ids, ",") != "claude:e1,claude:e2" {
		t.Errorf("BlobEvents = %q, %v", ids, err)
	}

	// SessionAttachments: metadata per event, no data.
	atts, err := s.SessionAttachments(ctx, "claude:s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(atts["claude:e1"]) != 2 || len(atts["claude:e2"]) != 2 || atts["claude:e2"][1].MediaType != "application/pdf" || atts["claude:e2"][1].Bytes != int64(len(testPDF)) ||
		atts["claude:e2"][0].Position != 0 || atts["claude:e2"][1].Position != 1 || atts["claude:e1"][1].SHA256 != blobref.Hash(testPNG) {
		t.Errorf("SessionAttachments = %+v", atts)
	}
	if atts, err := s.SessionAttachments(ctx, "claude:none"); err != nil || len(atts) != 0 {
		t.Errorf("SessionAttachments(none) = %+v, %v", atts, err)
	}

	// BlobStats for status.
	bs, err := s.BlobStats(ctx)
	if err != nil || bs.Count != 2 || bs.Bytes != int64(len(testPNG)+len(testPDF)) || bs.StoredBytes <= 0 || bs.StoredBytes >= bs.Bytes {
		t.Errorf("BlobStats = %+v, %v", bs, err)
	}

	// The read paths do not load the attachments.
	got, err := s.Events(ctx, "claude:s1", true)
	if err != nil || len(got) != 2 || len(got[0].Attachments) != 0 || len(got[1].Attachments) != 0 || got[0].Raw != "" {
		t.Errorf("Events = %+v, %v", got, err)
	}
}

func TestRawLineRestoresAttachments(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := attachedEvents()
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs); err != nil {
		t.Fatal(err)
	}
	line1 := `{"type":"user","uuid":"e1","message":{"content":[{"type":"tool_result","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + b64(testPNG) + `"}}]}]},"toolUseResult":{"file":{"base64":"` + b64(testPNG) + `","type":"image/png"}}}`
	line2 := `{"type":"user","uuid":"e2","message":{"content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + b64(testPNG) + `"}},{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` + b64(testPDF) + `"}}]}}`
	for id, want := range map[string]string{"claude:e1": line1, "claude:e2": line2} {
		got, err := s.RawLine(ctx, id)
		if err != nil || got != want {
			t.Errorf("RawLine(%s) = %q, %v\nwant %q", id, got, err, want)
		}
	}
	// The stored raw itself keeps the references: raw_text shows them.
	raw, _, ok, err := s.rawOf(ctx, "claude:e2")
	if err != nil || !ok || !strings.Contains(raw, blobref.Prefix) || strings.Contains(raw, b64(testPNG)) {
		t.Errorf("stored raw = %q, %v", raw, err)
	}
	if _, err := s.RawLine(ctx, "claude:none"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RawLine(unknown) = %v", err)
	}
	// An event without raw gives "".
	e3 := ev("claude:e3", "claude:s1", "2026-10-31T22:02:00Z", model.ActorHuman, model.KindMessage, "user", "x")
	e3.Raw = ""
	if err := s.WriteBatch(ctx, SourceState{}, nil, []model.Event{e3}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.RawLine(ctx, "claude:e3"); err != nil || got != "" {
		t.Errorf("RawLine(e3) = %q, %v", got, err)
	}
	// A reference whose blob is missing is an error that names it.
	e4 := e3
	e4.ID, e4.Raw = "claude:e4", `{"source":{"type":"base64","media_type":"image/png","data":"`+blobref.Ref{SHA256: strings.Repeat("ab", 32)}.String()+`"}}`
	if err := s.WriteBatch(ctx, SourceState{}, nil, []model.Event{e4}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RawLine(ctx, "claude:e4"); err == nil || !strings.Contains(err.Error(), strings.Repeat("ab", 32)) {
		t.Errorf("RawLine(e4) = %v", err)
	}
}

func TestRewriteRawReplacesAttachments(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := attachedEvents()
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs); err != nil {
		t.Fatal(err)
	}
	// e2 comes by again with a new line: only the document now.
	e2 := evs[1]
	e2.Raw = `{"type":"user","uuid":"e2","message":{"content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` + blobref.Ref{SHA256: blobref.Hash(testPDF)}.String() + `"}}]}}`
	e2.Attachments = evs[1].Attachments[1:]
	res, err := s.WriteBatchOpts(ctx, SourceState{}, nil, []model.Event{e2}, WriteOptions{RewriteRaw: true})
	if err != nil || res.RawRewritten != 1 || res.Blobs != 0 {
		t.Fatalf("rewrite: %+v, %v", res, err)
	}
	for q, want := range map[string]int{
		"SELECT count(*) FROM event_blobs WHERE event_id = 'claude:e2'":                                                               1,
		"SELECT count(*) FROM event_blobs WHERE event_id = 'claude:e2' AND position = 0 AND sha256 = '" + blobref.Hash(testPDF) + "'": 1,
		"SELECT count(*) FROM event_blobs": 3,
		"SELECT count(*) FROM blobs":       2,
	} {
		if got := countRows(t, s, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	// Without RewriteRaw an existing event keeps its attachments as they were.
	e2.Attachments = nil
	if _, err := s.WriteBatchOpts(ctx, SourceState{}, nil, []model.Event{e2}, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "SELECT count(*) FROM event_blobs WHERE event_id = 'claude:e2'"); n != 1 {
		t.Errorf("event_blobs of e2 after plain write = %d", n)
	}
	// A wrong hash is refused.
	bad := evs[0]
	bad.ID = "claude:e9"
	bad.Attachments = []model.Attachment{{SHA256: strings.Repeat("00", 32), MediaType: "image/png", Data: testPNG}}
	if _, err := s.WriteBatchOpts(ctx, SourceState{}, nil, []model.Event{bad}, WriteOptions{}); err == nil {
		t.Error("wrong hash: no error")
	}
}

func TestBlobsSurviveBackupAndMaintenance(t *testing.T) {
	s, p := openTestStore(t)
	sess, evs := attachedEvents()
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/a.jsonl", Harness: "claude", Size: 10, Offset: 10}, sess, evs); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(p)
	bak, err := s.Backup(ctx, filepath.Join(dir, "backups"), ts("2026-11-01T22:00:00Z"), 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	restored := filepath.Join(dir, "restored.db")
	if err := Restore(ctx, bak, restored, false); err != nil {
		t.Fatal(err)
	}
	check := func(path string) {
		t.Helper()
		st, err := OpenReadOnly(path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = st.Close() }()
		if _, data, err := st.Blob(ctx, blobref.Hash(testPDF)); err != nil || !bytes.Equal(data, testPDF) {
			t.Errorf("%s: Blob = %v", path, err)
		}
		if got, err := st.RawLine(ctx, "claude:e1"); err != nil || !strings.Contains(got, b64(testPNG)) {
			t.Errorf("%s: RawLine = %q, %v", path, got, err)
		}
		if ps, err := st.PageStats(ctx); err != nil || ps.Blobs != 2 {
			t.Errorf("%s: PageStats = %+v, %v", path, ps, err)
		}
	}
	check(restored)
	res, err := Maintain(ctx, p, maintainOpts(dir))
	if err != nil || !res.Swapped || res.After.Blobs != 2 || res.Before.Blobs != 2 {
		t.Fatalf("Maintain = %+v, %v", res, err)
	}
	check(p)
}

// TestPageStatsWithoutBlobsTable: status reads PageStats on an archive that
// waits for migration 6 and has no blobs table yet.
func TestPageStatsWithoutBlobsTable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "old.db")
	openAtVersion(t, p, 5)
	st, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if ps, err := st.PageStats(ctx); err != nil || ps.Blobs != 0 {
		t.Errorf("PageStats on v5 = %+v, %v", ps, err)
	}
}

func TestBlobEncoding(t *testing.T) {
	for mt, want := range map[string]string{"image/png": "", "image/jpeg": "", "image/gif": "", "image/webp": "", "video/mp4": "", "application/zip": "", "application/gzip": "",
		"application/pdf": "zstd", "text/plain": "zstd", "image/svg+xml": "zstd", "image/bmp": "zstd", "": "zstd"} {
		if got := blobEncoding(mt); got != want {
			t.Errorf("blobEncoding(%q) = %q, want %q", mt, got, want)
		}
	}
}
