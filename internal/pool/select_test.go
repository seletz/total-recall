package pool

import (
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/store"
)

func arch(folder, id, host string, at time.Time, schema int) Archive {
	return Archive{Entry: Entry{Host: folder, Path: "/pool/" + folder + "/archive.db.zst", At: at}, Path: "/cache/" + folder,
		Archive: store.Archive{ID: id, Host: host}, SchemaVersion: schema}
}

// TestSelect: every archive counts once, under the host it names: an
// archive without identity, an older copy of the same archive and two
// archives that claim the same host are left out with the reason.
func TestSelect(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	in := []Archive{
		arch("host-a", "a1", "host-a", t0, 11),
		arch("host-b", "b1", "host-b", t0, 11),
		arch("host-b-old", "b1", "host-b-old", t0.Add(-48*time.Hour), 11), // renamed, old folder left
		arch("legacy", "", "", t0, 10),
		arch("twin-1", "t1", "twin", t0, 11),
		arch("twin-2", "t2", "twin", t0, 11),
		arch("unnamed", "u1", "", t0, 11), // identity without host: the folder names it
	}
	keep, skipped := Select(in)
	var got []string
	for _, a := range keep {
		got = append(got, a.Host+">"+a.HostName())
	}
	if strings.Join(got, ",") != "host-a>host-a,host-b>host-b,unnamed>unnamed" {
		t.Errorf("keep = %v", got)
	}
	want := map[string]string{
		"host-b-old": "another copy of the archive of host-b, an older or equally new one",
		"legacy":     "schema v10 has no identity: run migrate on legacy, then backup",
		"twin-1":     "two archives claim host twin (twin-1, twin-2): set host on one of them",
		"twin-2":     "two archives claim host twin (twin-1, twin-2): set host on one of them",
	}
	if len(skipped) != len(want) {
		t.Fatalf("skipped = %+v", skipped)
	}
	for _, s := range skipped {
		if want[s.Host] != s.Reason {
			t.Errorf("skipped %s: %q, want %q", s.Host, s.Reason, want[s.Host])
		}
	}
}
