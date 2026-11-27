package store

import (
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/model"
)

// syncFixture writes two sessions with events, an attachment and a meta
// record into a fresh local store.
func syncFixture(t *testing.T, sess ...string) *Store {
	t.Helper()
	s, _ := openTestStore(t)
	png := []byte("\x89PNG fake image")
	for i, id := range sess {
		e1 := ev(id+"-e1", id, "2026-10-01T10:00:00.000Z", model.ActorHuman, model.KindMessage, "user", "deploy kompass please")
		e2 := ev(id+"-e2", id, "2026-10-01T10:05:00.000Z", model.ActorAgent, model.KindMessage, "assistant", "done, wetterwarte next")
		e2.TokensIn, e2.TokensOut, e2.Model = 100, 7, "model-x"
		if i == 0 {
			e1.Attachments = []model.Attachment{{SHA256: blobref.Hash(png), MediaType: "image/png", Data: png}}
		}
		meta := model.Meta{ID: id + "-m1", SessionID: id, Type: "pr-link", Raw: `{"type":"pr-link"}`, SourcePath: "/src/a.jsonl"}
		if _, err := s.WriteBatchOpts(ctx, SourceState{Path: "/src/a.jsonl", Harness: "claude", Offset: 10},
			[]model.Session{{ID: id, Harness: "claude", NativeID: id[len("claude:"):], Cwd: "/home/mara/kompass", Name: "T " + id}},
			[]model.Event{e1, e2}, WriteOptions{Meta: []model.Meta{meta}}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestExportImport(t *testing.T) {
	local := syncFixture(t, "claude:s1", "claude:s2")
	berlin, _ := time.LoadLocation("Europe/Berlin")
	ny, _ := time.LoadLocation("America/New_York")
	central, _ := openTestStore(t, WithLocation(ny))
	_ = berlin

	var st SyncState
	var total ImportResult
	batches := 0
	for {
		cs, err := local.Export(ctx, &st, 3)
		if err != nil {
			t.Fatal(err)
		}
		if cs.Empty() {
			break
		}
		batches++
		r, err := central.Import(ctx, "host-a", cs)
		if err != nil {
			t.Fatal(err)
		}
		total.Add(r)
		st.Commit(cs)
	}
	if batches < 2 {
		t.Errorf("batches = %d, want the limit to split the export", batches)
	}
	want := ImportResult{Sessions: 2, Events: 4, Meta: 2, Blobs: 1}
	if total != want {
		t.Errorf("import = %+v, want %+v", total, want)
	}
	for q, n := range map[string]int{
		"SELECT count(*) FROM sessions WHERE host = 'host-a'":                          2,
		"SELECT count(*) FROM events":                                                  4,
		"SELECT count(*) FROM events_fts WHERE events_fts MATCH 'wetterwarte'":         2,
		"SELECT count(*) FROM event_blobs":                                             1,
		"SELECT count(*) FROM session_meta":                                            2,
		"SELECT count(*) FROM sources WHERE path = 'host-a:/src/a.jsonl'":              1,
		"SELECT count(*) FROM events WHERE ts_local = '2026-10-01T06:00:00.000-04:00'": 2,
		"SELECT count(*) FROM events WHERE tokens_in = 100 AND model = 'model-x'":      2,
	} {
		if got := countRows(t, central, q); got != n {
			t.Errorf("%s = %d, want %d", q, got, n)
		}
	}
	// The raw line survives byte for byte.
	if got, err := central.RawLine(ctx, "claude:s1-e1"); err != nil || got != `{"x":1}` {
		t.Errorf("RawLine = %q, %v", got, err)
	}

	// Nothing new: the next export is empty.
	if cs, err := local.Export(ctx, &st, 3); err != nil || !cs.Empty() {
		t.Errorf("export after commit: %d events, %d sessions, %v", len(cs.Events), len(cs.Sessions), err)
	}

	// A renamed session is sent again without new events.
	if _, err := local.WriteBatchOpts(ctx, SourceState{}, []model.Session{{ID: "claude:s2", Harness: "claude", Name: "renamed"}}, nil, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	cs, err := local.Export(ctx, &st, 3)
	if err != nil || len(cs.Sessions) != 1 || len(cs.Events) != 0 {
		t.Fatalf("export after rename: %d sessions, %d events, %v", len(cs.Sessions), len(cs.Events), err)
	}
	if _, err := central.Import(ctx, "host-a", cs); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, central, "SELECT count(*) FROM sessions WHERE name = 'renamed'"); got != 1 {
		t.Errorf("renamed sessions = %d", got)
	}

	// Delivering the same batch twice (lost acknowledgement) changes nothing.
	again, err := central.Import(ctx, "host-a", cs)
	if err != nil || again.Events != 0 || again.Conflicts != 0 {
		t.Errorf("re-import = %+v, %v", again, err)
	}
}

func TestImportConflict(t *testing.T) {
	// A host that has the same session (a cloned VM image) must not
	// silently merge into another host's session.
	a := syncFixture(t, "claude:s1")
	central, _ := openTestStore(t)
	for _, host := range []string{"host-a", "host-b"} {
		var st SyncState
		cs, err := a.Export(ctx, &st, 0)
		if err != nil {
			t.Fatal(err)
		}
		r, err := central.Import(ctx, host, cs)
		if err != nil {
			t.Fatal(err)
		}
		if host == "host-b" && (r.Conflicts != 1 || r.Events != 0 || r.Sessions != 0) {
			t.Errorf("host-b import = %+v, want one conflict and nothing written", r)
		}
	}
	if got := countRows(t, central, "SELECT count(*) FROM sessions WHERE host = 'host-a'"); got != 1 {
		t.Errorf("sessions of host-a = %d", got)
	}
}

func TestImportRejectsBadBlob(t *testing.T) {
	local := syncFixture(t, "claude:s1")
	central, _ := openTestStore(t)
	var st SyncState
	cs, err := local.Export(ctx, &st, 0)
	if err != nil {
		t.Fatal(err)
	}
	cs.Blobs[0].Data = []byte("tampered")
	if _, err := central.Import(ctx, "host-a", cs); err == nil {
		t.Error("import of a blob whose hash does not match: no error")
	}
	if got := countRows(t, central, "SELECT count(*) FROM events"); got != 0 {
		t.Errorf("events after failed import = %d, want 0 (one transaction)", got)
	}
}

func TestImportNewerSchema(t *testing.T) {
	central, _ := openTestStore(t)
	if _, err := central.Import(ctx, "host-a", Changeset{Schema: LatestSchemaVersion() + 1}); err == nil {
		t.Error("changeset of a newer schema accepted")
	}
}
