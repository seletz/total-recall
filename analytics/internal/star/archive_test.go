package star

import (
	"path/filepath"
	"testing"
)

// TestArchiveIdentity: the watermark keeps the archive ID of each source.
// A host renamed in the config keeps its archive: the next run under the
// new name replaces the old source instead of counting the archive twice.
// A new archive under a known host name (reinstalled machine) is loaded
// in full instead of continuing at a watermark that belongs to the old
// archive.
func TestArchiveIdentity(t *testing.T) {
	a := demoCopy(t)
	db := openDuck(t, filepath.Join(t.TempDir(), "a.duckdb"))
	o := opts(t, a)
	o.ArchiveID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	mustRun(t, db, o)
	want := snapshot(t, db)
	events := row(t, db, "SELECT count(*) FROM analytics_events_v1")

	// Renamed: same archive, new host name.
	o.Source, o.Host = "host-renamed", "host-renamed"
	r := mustRun(t, db, o)
	if r.Mode != "full" || len(r.Replaced) != 1 || r.Replaced[0] != "host-a" {
		t.Errorf("rename = mode %s, replaced %v", r.Mode, r.Replaced)
	}
	if got := row(t, db, "SELECT count(*) FROM analytics_events_v1"); got != events {
		t.Errorf("events after the rename = %s, want %s (counted once)", got, events)
	}
	if got := row(t, db, "SELECT string_agg(DISTINCT host, ',') FROM analytics_sessions_v1"); got != "host-renamed" {
		t.Errorf("hosts after the rename = %s", got)
	}
	if got := row(t, db, "SELECT string_agg(source || '/' || archive_id, ',') FROM etl_watermark"); got != "host-renamed/"+o.ArchiveID {
		t.Errorf("watermarks = %s", got)
	}

	// New archive under the same name: full reload, not incremental.
	o.ArchiveID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if r := mustRun(t, db, o); r.Mode != "full" || r.SeqFrom != 0 {
		t.Errorf("new archive = mode %s from %d, want a full load", r.Mode, r.SeqFrom)
	}
	// Same archive again: incremental, nothing new.
	if r := mustRun(t, db, o); r.Mode != "incremental" || r.Events != 0 {
		t.Errorf("same archive again = %+v", r)
	}
	// Back under the first name: the data is the first build's.
	o.Source, o.Host = "host-a", "host-a"
	mustRun(t, db, o)
	compareSnapshots(t, "after rename and back", want, snapshot(t, db))
}
