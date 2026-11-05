package store

import (
	"bytes"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/model"
)

func metaRecord(id, sess, typ, raw string) model.Meta {
	return model.Meta{ID: id, SessionID: sess, TS: ts("2026-10-01T10:00:00Z"), Type: typ, SourcePath: "/src/a.jsonl", SourceOffset: 7, Raw: raw}
}

// TestWriteMeta: meta records land in session_meta compressed and come
// back through metaOf and raw_text; a second write of the same ID is
// skipped (reread), a record of an unknown session creates a bare session
// row without times, and a record without ID, session or content is
// refused.
func TestWriteMeta(t *testing.T) {
	s, _ := openTestStore(t)
	line := `{"type":"pr-link","sessionId":"s1","prNumber":7,"prUrl":"https://git.invalid/demo/repo/pull/7"}`
	metas := []model.Meta{
		metaRecord("claude:m1", "claude:s1", "pr-link", line),
		metaRecord("claude:m2", "claude:s1", "mode", `{"type":"mode","mode":"normal"}`),
	}
	metas[1].TS = ts("0001-01-01T00:00:00Z")
	res, err := s.WriteBatchOpts(ctx, SourceState{}, nil, nil, WriteOptions{Meta: metas})
	if err != nil {
		t.Fatal(err)
	}
	if res.MetaInserted != 2 || res.Inserted != 0 {
		t.Errorf("result = %+v", res)
	}
	m, ok, err := s.metaOf(ctx, "claude:m1")
	if err != nil || !ok {
		t.Fatalf("metaOf = %v, %v", ok, err)
	}
	if m.Raw != line || m.Type != "pr-link" || m.SessionID != "claude:s1" || !m.TS.Equal(ts("2026-10-01T10:00:00Z")) || m.SourcePath != "/src/a.jsonl" || m.SourceOffset != 7 {
		t.Errorf("meta = %+v", m)
	}
	checks := map[string]int{
		"SELECT count(*) FROM session_meta WHERE typeof(raw) = 'blob'":                                                            2,
		"SELECT count(*) FROM session_meta WHERE id = 'claude:m2' AND ts IS NULL":                                                 1,
		"SELECT count(*) FROM session_meta WHERE raw_text(raw) LIKE '%pull/7%'":                                                   1,
		"SELECT count(*) FROM session_meta m JOIN sources s ON s.id = m.source_id WHERE s.path = '/src/a.jsonl'":                  2,
		"SELECT count(*) FROM sessions WHERE id = 'claude:s1' AND harness = 'claude' AND native_id = 's1' AND started_at IS NULL": 1,
		"SELECT count(*) FROM events": 0,
	}
	for q, want := range checks {
		if got := countRows(t, s, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	// Reread: the same IDs are skipped, nothing is counted.
	res, err = s.WriteBatchOpts(ctx, SourceState{}, nil, nil, WriteOptions{Meta: metas})
	if err != nil || res.MetaInserted != 0 || res.RawRewritten != 0 {
		t.Errorf("second write = %+v, %v", res, err)
	}
	if n := countRows(t, s, "SELECT count(*) FROM session_meta"); n != 2 {
		t.Errorf("meta rows after reread = %d", n)
	}
	// RewriteRaw replaces raw of the existing record and inserts a new one.
	metas[0].Raw = `{"type":"pr-link","prNumber":7,"prUrl":"https://git.invalid/demo/repo/pull/7","note":"rewritten"}`
	metas = append(metas, metaRecord("claude:m3", "claude:s1", "tools-used", `{"type":"tools-used"}`))
	res, err = s.WriteBatchOpts(ctx, SourceState{}, nil, nil, WriteOptions{Meta: metas, RewriteRaw: true})
	if err != nil || res.MetaInserted != 1 || res.RawRewritten != 2 {
		t.Errorf("rewrite = %+v, %v", res, err)
	}
	if m, _, _ := s.metaOf(ctx, "claude:m1"); !strings.Contains(m.Raw, "rewritten") {
		t.Errorf("raw after rewrite = %q", m.Raw)
	}
	for _, bad := range []model.Meta{
		{ID: "", SessionID: "claude:s1", Type: "x", Raw: "{}"},
		{ID: "claude:x", SessionID: "", Type: "x", Raw: "{}"},
		{ID: "claude:x", SessionID: "claude:s1", Type: "x", Raw: ""},
	} {
		if _, err := s.WriteBatchOpts(ctx, SourceState{}, nil, nil, WriteOptions{Meta: []model.Meta{bad}}); err == nil {
			t.Errorf("write of %+v succeeded", bad)
		}
	}
	if _, ok, err := s.metaOf(ctx, "claude:nope"); ok || err != nil {
		t.Errorf("metaOf unknown = %v, %v", ok, err)
	}
}

// TestMetaStats counts the meta records and their compressed size, and
// PageStats reports the rows for the maintenance check.
func TestMetaStats(t *testing.T) {
	s, _ := openTestStore(t)
	st, err := s.MetaStats(ctx)
	if err != nil || st.Rows != 0 || st.Bytes != 0 {
		t.Fatalf("empty: %+v, %v", st, err)
	}
	line := `{"type":"attachment","attachment":{"type":"file","content":"` + strings.Repeat("compressible text ", 50) + `"}}`
	if _, err := s.WriteBatchOpts(ctx, SourceState{}, nil, nil, WriteOptions{Meta: []model.Meta{
		metaRecord("claude:m1", "claude:s1", "attachment", line),
		metaRecord("claude:m2", "claude:s2", "attachment", line),
	}}); err != nil {
		t.Fatal(err)
	}
	st, err = s.MetaStats(ctx)
	if err != nil || st.Rows != 2 || st.Bytes <= 0 || st.Bytes >= int64(2*len(line)) {
		t.Errorf("MetaStats = %+v, %v", st, err)
	}
	ps, err := s.PageStats(ctx)
	if err != nil || ps.Meta != 2 || ps.Sessions != 2 {
		t.Errorf("PageStats = %+v, %v", ps, err)
	}
}

// TestMetaSessionTouched: a meta record of an existing session leaves its
// times and titles alone.
func TestMetaSessionTouched(t *testing.T) {
	s, _ := openTestStore(t)
	sess := model.Session{ID: "claude:s1", Harness: "claude", NativeID: "s1", Name: "Title", StartedAt: ts("2026-10-01T10:00:00Z"), EndedAt: ts("2026-10-01T11:00:00Z")}
	if err := s.WriteBatch(ctx, SourceState{}, []model.Session{sess}, nil); err != nil {
		t.Fatal(err)
	}
	late := metaRecord("claude:m1", "claude:s1", "mode", `{"type":"mode"}`)
	late.TS = ts("2026-10-02T10:00:00Z")
	if _, err := s.WriteBatchOpts(ctx, SourceState{}, nil, nil, WriteOptions{Meta: []model.Meta{late}}); err != nil {
		t.Fatal(err)
	}
	got, err := getSession(ctx, s.db(), "claude:s1")
	if err != nil || got.Name != "Title" || !got.StartedAt.Equal(sess.StartedAt) || !got.EndedAt.Equal(sess.EndedAt) {
		t.Errorf("session after meta = %+v, %v", got, err)
	}
}

// TestWriteMetaAttachments: the attachments of meta records go to blobs
// once per hash and are linked in meta_blobs; a reread writes nothing
// twice; a rewrite replaces the links; BlobMeta lists the records of a
// blob; a wrong hash is refused.
func TestWriteMetaAttachments(t *testing.T) {
	s, _ := openTestStore(t)
	shape := []byte(`{"system":[{"type":"text","text":"` + strings.Repeat("be brief ", 30) + `"}],"tools":[]}`)
	att := model.Attachment{SHA256: blobref.Hash(shape), MediaType: "application/json", Data: shape}
	ref := `"` + blobref.Ref{SHA256: att.SHA256}.String() + `"`
	m1 := metaRecord("claude:m1", "claude:s1", "api-request-shape", `{"type":"api-request-shape","shape":`+ref+`}`)
	m2 := metaRecord("claude:m2", "claude:s2", "api-request-shape", `{"type":"api-request-shape","shape":`+ref+`}`)
	m1.Attachments, m2.Attachments = []model.Attachment{att}, []model.Attachment{att}
	res, err := s.WriteBatchOpts(ctx, SourceState{}, nil, nil, WriteOptions{Meta: []model.Meta{m1, m2}})
	if err != nil {
		t.Fatal(err)
	}
	if res.MetaInserted != 2 || res.Blobs != 1 || res.BlobBytes != int64(len(shape)) {
		t.Errorf("result = %+v", res)
	}
	for q, want := range map[string]int{
		"SELECT count(*) FROM blobs": 1,
		"SELECT count(*) FROM blobs WHERE media_type = 'application/json' AND encoding = 'zstd' AND bytes = " + itoa(len(shape)) + " AND length(data) < bytes": 1,
		"SELECT count(*) FROM meta_blobs": 2,
		"SELECT count(*) FROM meta_blobs WHERE meta_id = 'claude:m2' AND position = 0 AND sha256 = '" + att.SHA256 + "'": 1,
	} {
		if got := countRows(t, s, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	if ids, err := s.BlobMeta(ctx, att.SHA256); err != nil || len(ids) != 2 || ids[0] != "claude:m1" || ids[1] != "claude:m2" {
		t.Errorf("BlobMeta = %v, %v", ids, err)
	}
	if _, data, err := s.Blob(ctx, att.SHA256); err != nil || !bytes.Equal(data, shape) {
		t.Errorf("Blob = %q, %v", data, err)
	}
	// Reread: nothing new.
	res, err = s.WriteBatchOpts(ctx, SourceState{}, nil, nil, WriteOptions{Meta: []model.Meta{m1}})
	if err != nil || res.MetaInserted != 0 || res.Blobs != 0 || countRows(t, s, "SELECT count(*) FROM meta_blobs") != 2 {
		t.Errorf("reread = %+v, %v", res, err)
	}
	// Rewrite with another shape: the link follows, the old blob stays.
	shape2 := []byte(`{"system":[],"tools":[{"name":"Read"}]}`)
	m1.Attachments = []model.Attachment{{SHA256: blobref.Hash(shape2), MediaType: "application/json", Data: shape2}}
	m1.Raw = `{"type":"api-request-shape","shape":"` + blobref.Ref{SHA256: blobref.Hash(shape2)}.String() + `"}`
	res, err = s.WriteBatchOpts(ctx, SourceState{}, nil, nil, WriteOptions{Meta: []model.Meta{m1}, RewriteRaw: true})
	if err != nil || res.RawRewritten != 1 || res.Blobs != 1 {
		t.Errorf("rewrite = %+v, %v", res, err)
	}
	if got := countRows(t, s, "SELECT count(*) FROM meta_blobs WHERE meta_id = 'claude:m1' AND sha256 = '"+blobref.Hash(shape2)+"'"); got != 1 {
		t.Errorf("link after rewrite = %d", got)
	}
	if ids, _ := s.BlobMeta(ctx, att.SHA256); len(ids) != 1 || ids[0] != "claude:m2" {
		t.Errorf("BlobMeta after rewrite = %v", ids)
	}
	// A hash that does not match its data is refused.
	bad := metaRecord("claude:m3", "claude:s1", "api-request-shape", `{}`)
	bad.Attachments = []model.Attachment{{SHA256: "0000", MediaType: "application/json", Data: shape}}
	if _, err := s.WriteBatchOpts(ctx, SourceState{}, nil, nil, WriteOptions{Meta: []model.Meta{bad}}); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Errorf("bad hash: %v", err)
	}
}

// TestWrittenRowsCountOncePerRun: a row written again in a later batch of
// the run counts once, a row the run inserted does not count as rewritten,
// and a batch that rolled back leaves nothing behind (#201).
func TestWrittenRowsCountOncePerRun(t *testing.T) {
	s, _ := openTestStore(t)
	sess := "claude:s1"
	ev := func(id string) model.Event {
		return model.Event{ID: id, SessionID: sess, Kind: model.KindMessage, Raw: `{"a":1}`}
	}
	meta := func(id string) model.Meta {
		return model.Meta{ID: id, SessionID: sess, Type: "mode", Raw: `{"m":1}`}
	}
	write := func(w *WrittenRows, evs []model.Event, ms ...model.Meta) WriteResult {
		t.Helper()
		res, err := s.WriteBatchOpts(ctx, SourceState{}, nil, evs, WriteOptions{RewriteRaw: true, Meta: ms, Written: w})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	write(nil, []model.Event{ev("claude:e1")}, meta("claude:m1"))

	w := NewWrittenRows()
	if res := write(w, []model.Event{ev("claude:e1"), ev("claude:e1"), ev("claude:e2")}, meta("claude:m1"), meta("claude:m1"), meta("claude:m2")); res.RawRewritten != 2 || res.MetaRewritten != 1 || res.Inserted != 1 || res.MetaInserted != 1 {
		t.Errorf("first batch = %+v", res)
	}
	if res := write(w, []model.Event{ev("claude:e1"), ev("claude:e2")}, meta("claude:m1"), meta("claude:m2")); res.RawRewritten != 0 || res.MetaRewritten != 0 {
		t.Errorf("second batch = %+v, want nothing counted", res)
	}

	// The attachment fails after e3 was counted: the retry counts it.
	w = NewWrittenRows()
	write(nil, []model.Event{ev("claude:e3")})
	bad := ev("claude:e3")
	bad.Attachments = []model.Attachment{{SHA256: "00", MediaType: "image/png", Data: []byte("x")}}
	if _, err := s.WriteBatchOpts(ctx, SourceState{}, nil, []model.Event{bad}, WriteOptions{RewriteRaw: true, Written: w}); err == nil {
		t.Fatal("hash mismatch not refused")
	}
	if res := write(w, []model.Event{ev("claude:e3")}); res.RawRewritten != 1 {
		t.Errorf("retry = %+v, want the row counted", res)
	}
}
