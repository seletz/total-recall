package store

import (
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

// TestRawStoredCompressed: raw goes in as text and is stored as a zstd
// blob; rawOf, UserMessages and the SQL function raw_text give the text
// back; an empty raw is NULL.
func TestRawStoredCompressed(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := sampleBatch()
	long := `{"type":"user","message":{"content":"` + strings.Repeat("the same words again and again ", 200) + `"}}`
	evs[0].Raw = long
	evs[1].Raw = ""
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		id        string
		want      string
		truncated bool
	}{
		{"claude:e1", long, false},
		{"claude:e2", "", false},
		{"claude:e3", `{"x":1}`, false},
	}
	for _, tt := range tests {
		raw, trunc, ok, err := s.rawOf(ctx, tt.id)
		if err != nil || !ok || raw != tt.want || trunc != tt.truncated {
			t.Errorf("rawOf(%s) = %q, %v, %v, %v", tt.id, raw, trunc, ok, err)
		}
	}
	if _, _, ok, _ := s.rawOf(ctx, "claude:nope"); ok {
		t.Error("rawOf unknown id: ok")
	}
	if n := countRows(t, s, "SELECT count(*) FROM events WHERE id = 'claude:e1' AND typeof(raw) = 'blob' AND length(raw) < 400"); n != 1 {
		t.Errorf("e1 not stored as a small blob")
	}
	if n := countRows(t, s, "SELECT count(*) FROM events WHERE id = 'claude:e2' AND raw IS NULL"); n != 1 {
		t.Error("empty raw not NULL")
	}
	checks := map[string]int{
		"SELECT count(*) FROM events WHERE raw_text(raw) = '" + long + "'":                                                         1,
		"SELECT count(*) FROM events WHERE raw_text(raw) IS NULL":                                                                  1,
		"SELECT count(*) FROM events WHERE raw_text(raw) = '{\"x\":1}'":                                                            len(evs) - 2,
		"SELECT count(*) FROM (SELECT raw_text('plain text') AS t) WHERE t = 'plain text'":                                         1,
		"SELECT count(*) FROM (SELECT raw_text(CAST('{}' AS BLOB)) AS t) WHERE t = '{}'":                                           1,
		"SELECT count(*) FROM (SELECT raw_text(NULL) AS t) WHERE t IS NULL":                                                        1,
		"SELECT count(*) FROM (SELECT length(raw_text(raw)) AS n FROM events WHERE id = 'claude:e1') WHERE n = " + itoa(len(long)): 1,
	}
	for q, want := range checks {
		if got := countRows(t, s, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + fmtInt(n)) }

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestSourceIDs: events reference sources by id; an event of a path that
// is not an ingested source creates a bare sources row; the read path
// returns the path again.
func TestSourceIDs(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := sampleBatch()
	evs[1].SourcePath = "/src/other.jsonl"
	evs[2].SourcePath = ""
	src := SourceState{Path: "/src/a.jsonl", Harness: "claude", Size: 10, Offset: 10}
	if err := s.WriteBatch(ctx, src, sess, evs); err != nil {
		t.Fatal(err)
	}
	checks := map[string]int{
		"SELECT count(*) FROM sources": 2,
		"SELECT count(*) FROM sources WHERE path = '/src/a.jsonl' AND \"offset\" = 10 AND size = 10":             1,
		"SELECT count(*) FROM sources WHERE path = '/src/other.jsonl' AND \"offset\" = 0 AND harness = 'claude'": 1,
		"SELECT count(*) FROM events WHERE source_id IS NULL":                                                    1,
		"SELECT count(DISTINCT source_id) FROM events WHERE source_id IS NOT NULL":                               2,
	}
	for q, want := range checks {
		if got := countRows(t, s, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	got, err := s.Events(ctx, "claude:s1", true)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, e := range got {
		paths[e.ID] = e.SourcePath
	}
	for id, want := range map[string]string{"claude:e1": "/src/a.jsonl", "claude:e2": "/src/other.jsonl", "claude:e3": ""} {
		if paths[id] != want {
			t.Errorf("%s source path = %q, want %q", id, paths[id], want)
		}
	}
	// The source row of a later ingest step fills the bare row; nothing is duplicated.
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/other.jsonl", Harness: "claude", Size: 99, Offset: 99}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "SELECT count(*) FROM sources WHERE path = '/src/other.jsonl' AND \"offset\" = 99"); n != 1 {
		t.Error("bare source row not updated")
	}
	if n := countRows(t, s, "SELECT count(*) FROM sources"); n != 2 {
		t.Errorf("sources = %d", n)
	}
	// Foreign keys hold: a sources row with events cannot go.
	if _, err := s.db().ExecContext(ctx, "DELETE FROM sources WHERE path = '/src/a.jsonl'"); err == nil {
		t.Error("deleting a referenced source succeeded")
	}
}

func TestRawStats(t *testing.T) {
	s, _ := openTestStore(t)
	st, err := s.RawStats(ctx)
	if err != nil || st.Rows != 0 || st.Bytes != 0 || st.SampleRows != 0 {
		t.Fatalf("empty: %+v, %v", st, err)
	}
	sess, evs := sampleBatch()
	line := `{"type":"user","message":{"content":"` + strings.Repeat("compressible text ", 50) + `"}}`
	for i := range evs {
		evs[i].Raw = line
	}
	evs[0].Raw, evs[0].RawTruncated = "", true
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs); err != nil {
		t.Fatal(err)
	}
	st, err = s.RawStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n := int64(len(evs) - 1)
	if st.Rows != n || st.Truncated != 1 || st.SampleRows != n || st.Bytes <= 0 || st.Bytes >= n*int64(len(line)) {
		t.Errorf("RawStats = %+v", st)
	}
	if st.Ratio < 2 {
		t.Errorf("ratio = %.2f, want a clearly compressed sample", st.Ratio)
	}
}

var _ = model.KindMessage
