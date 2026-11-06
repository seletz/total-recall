package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// namesZone is a fixed zone, so the tests do not depend on the machine's
// zone and the zone of a name differs from UTC.
var namesZone = time.FixedZone("CET", 3600)

// copyAt is a backup file with a set modification time.
type copyAt struct {
	name  string
	mtime time.Time
}

func writeCopies(t *testing.T, dir string, copies ...copyAt) {
	t.Helper()
	for _, c := range copies {
		writeBackups(t, dir, c.name)
		if err := os.Chtimes(filepath.Join(dir, c.name), c.mtime, c.mtime); err != nil {
			t.Fatal(err)
		}
	}
}

func at(day, clock string) time.Time {
	v, err := time.ParseInLocation("2006-01-02 15:04", day+" "+clock, namesZone)
	if err != nil {
		panic(err)
	}
	return v
}

func TestBackupTime(t *testing.T) {
	mtime := at("2026-11-05", "22:03")
	tests := []struct {
		name  string
		file  string
		mtime time.Time
		want  time.Time
	}{
		{"time in the name beats mtime", "archive-2026-11-05-1505.db.zst", mtime, at("2026-11-05", "15:05")},
		{"numbered name", "archive-2026-11-05-1505-2.db.zst", mtime, at("2026-11-05", "15:05")},
		{"gzip with time", "archive-2026-11-05-1505.db.gz", mtime, at("2026-11-05", "15:05")},
		{"plain with time", "archive-2026-11-05-1505.db", mtime, at("2026-11-05", "15:05")},
		{"day name takes mtime", "archive-2026-11-05.db.zst", mtime, mtime},
		{"gzip day name takes mtime", "archive-2026-11-05.db.gz", mtime, mtime},
		{"plain day name takes mtime", "archive-2026-11-05.db", mtime, mtime},
		{"mtime on the day only in the zone of the names", "archive-2026-11-05.db.zst", time.Date(2026, 11, 4, 23, 30, 0, 0, time.UTC), time.Date(2026, 11, 4, 23, 30, 0, 0, time.UTC)},
		{"copied later: start of the named day", "archive-2026-11-05.db.zst", at("2026-11-06", "09:00"), at("2026-11-05", "00:00")},
		{"mtime before the day: start of the named day", "archive-2026-11-05.db.zst", at("2026-11-04", "23:59"), at("2026-11-05", "00:00")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, ok := parseBackupName(tt.file, namesZone)
			if !ok {
				t.Fatalf("%s not recognized", tt.file)
			}
			if got := backupTime(b, tt.mtime); !got.Equal(tt.want) {
				t.Errorf("backupTime = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestParseBackupNameRejectsImpossibleTimes: a name without a real date and
// time is not a backup copy, so nothing is decided from it and retention
// leaves it alone.
func TestParseBackupNameRejectsImpossibleTimes(t *testing.T) {
	for _, n := range []string{"archive-2026-13-01.db.zst", "archive-2026-02-30.db.zst", "archive-2026-11-05-2561.db.zst"} {
		if _, ok := parseBackupName(n, namesZone); ok {
			t.Errorf("%s recognized", n)
		}
	}
}

func TestLastBackupByTime(t *testing.T) {
	day := "2026-11-05"
	tests := []struct {
		name   string
		copies []copyAt
		want   string
		wantAt time.Time
	}{
		{"the issue: day name later than the timed copy", []copyAt{
			{"archive-2026-11-05-1505.db.gz", at(day, "15:05")},
			{"archive-2026-11-05.db.zst", at(day, "22:03")},
		}, "archive-2026-11-05.db.zst", at(day, "22:03")},
		{"day name earlier than the timed copy", []copyAt{
			{"archive-2026-11-05.db.zst", at(day, "08:00")},
			{"archive-2026-11-05-1505.db.zst", at(day, "15:05")},
		}, "archive-2026-11-05-1505.db.zst", at(day, "15:05")},
		{"mtime of a timed copy does not count", []copyAt{
			{"archive-2026-11-05-1505.db.zst", at(day, "23:00")},
			{"archive-2026-11-05.db.zst", at(day, "22:03")},
		}, "archive-2026-11-05.db.zst", at(day, "22:03")},
		{"day names of two formats", []copyAt{
			{"archive-2026-11-05.db.gz", at(day, "21:00")},
			{"archive-2026-11-05.db", at(day, "08:00")},
			{"archive-2026-11-05.db.zst", at(day, "07:00")},
		}, "archive-2026-11-05.db.gz", at(day, "21:00")},
		{"all formats and names on one day", []copyAt{
			{"archive-2026-11-05.db", at(day, "06:00")},
			{"archive-2026-11-05-0900.db.gz", at(day, "09:00")},
			{"archive-2026-11-05-0900-2.db.zst", at(day, "09:00")},
			{"archive-2026-11-05.db.zst", at(day, "12:00")},
			{"archive-2026-11-05-1800.db.gz", at(day, "18:00")},
			{"archive-2026-11-05-1800-2.db.gz", at(day, "18:00")},
			{"archive-2026-11-05.db.gz", at(day, "17:00")},
		}, "archive-2026-11-05-1800-2.db.gz", at(day, "18:00")},
		// The day name was finished 40 s into the minute, the next backup
		// of that minute is named after its full minute.
		{"same minute: the timed copy came second", []copyAt{
			{"archive-2026-11-05.db.zst", at(day, "09:30").Add(40 * time.Second)},
			{"archive-2026-11-05-0930.db.zst", at(day, "09:30").Add(50 * time.Second)},
		}, "archive-2026-11-05-0930.db.zst", at(day, "09:30")},
		{"older day with a late clock", []copyAt{
			{"archive-2026-11-04-2359.db.zst", at("2026-11-04", "23:59")},
			{"archive-2026-11-05.db.gz", at(day, "00:10")},
		}, "archive-2026-11-05.db.gz", at(day, "00:10")},
		{"day name copied to a new machine stays on its day", []copyAt{
			{"archive-2026-11-04.db.zst", at("2026-11-06", "09:00")},
			{"archive-2026-11-05-1200.db.zst", at("2026-11-06", "09:00")},
		}, "archive-2026-11-05-1200.db.zst", at(day, "12:00")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCopies(t, dir, tt.copies...)
			p, got, err := LastBackup(dir, namesZone)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(p) != tt.want {
				t.Errorf("LastBackup = %s, want %s", filepath.Base(p), tt.want)
			}
			if !got.Equal(tt.wantAt) {
				t.Errorf("time = %v, want %v", got, tt.wantAt)
			}
		})
	}
}

// TestLastBackupNameInConfiguredZone: the clock in a name is local to the
// configured zone, the modification time is absolute. 15:05 in that zone is
// before 14:30 UTC.
func TestLastBackupNameInConfiguredZone(t *testing.T) {
	dir := t.TempDir()
	writeCopies(t, dir,
		copyAt{"archive-2026-11-05-1505.db.gz", at("2026-11-05", "15:05")},
		copyAt{"archive-2026-11-05.db.zst", time.Date(2026, 11, 5, 14, 30, 0, 0, time.UTC)},
	)
	p, _, err := LastBackup(dir, namesZone)
	if err != nil || filepath.Base(p) != "archive-2026-11-05.db.zst" {
		t.Errorf("LastBackup = %q, %v", p, err)
	}
}

// TestPruneKeepsNewestOfMonthByTime: the copy retention keeps for a month is
// the newest by time, not the one that sorts last by name.
func TestPruneKeepsNewestOfMonthByTime(t *testing.T) {
	dir := t.TempDir()
	writeCopies(t, dir,
		copyAt{"archive-2026-10-31-1505.db.gz", at("2026-10-31", "15:05")},
		copyAt{"archive-2026-10-31.db.zst", at("2026-10-31", "22:03")},
		copyAt{"archive-2026-11-01.db.zst", at("2026-11-01", "22:03")},
	)
	removed, err := PruneBackups(dir, 1, 0, namesZone)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || filepath.Base(removed[0]) != "archive-2026-10-31-1505.db.gz" {
		t.Errorf("removed %v", removed)
	}
	want := []string{"archive-2026-10-31.db.zst", "archive-2026-11-01.db.zst"}
	if got := listBackups(t, dir); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("kept %v, want %v", got, want)
	}
}

// TestPruneOrderOfDays: copies of one day stay together, also when the
// modification time of a day name would put it elsewhere.
func TestPruneOrderOfDays(t *testing.T) {
	dir := t.TempDir()
	writeCopies(t, dir,
		copyAt{"archive-2026-11-03.db.zst", at("2026-11-06", "09:00")}, // copied later
		copyAt{"archive-2026-11-04.db.zst", at("2026-11-04", "08:00")},
		copyAt{"archive-2026-11-05-1200.db.zst", at("2026-11-05", "12:00")},
		copyAt{"archive-2026-11-05.db.zst", at("2026-11-05", "22:00")},
	)
	if _, err := PruneBackups(dir, 2, 1, namesZone); err != nil {
		t.Fatal(err)
	}
	want := []string{"archive-2026-11-04.db.zst", "archive-2026-11-05-1200.db.zst", "archive-2026-11-05.db.zst"}
	if got := listBackups(t, dir); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("kept %v, want %v", got, want)
	}
}

// TestBackupRetentionUsesZoneOfNow: Backup decides in the zone of now, which
// is the zone the names are written in.
func TestBackupRetentionUsesZoneOfNow(t *testing.T) {
	s, _ := openTestStore(t)
	dir := t.TempDir()
	writeCopies(t, dir,
		copyAt{"archive-2026-10-31-1505.db.gz", at("2026-10-31", "15:05")},
		copyAt{"archive-2026-10-31.db.zst", at("2026-10-31", "22:03")},
	)
	if _, err := s.Backup(ctx, dir, at("2026-11-01", "09:00"), 1, 0); err != nil {
		t.Fatal(err)
	}
	want := []string{"archive-2026-10-31.db.zst", "archive-2026-11-01.db.zst"}
	if got := listBackups(t, dir); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("kept %v, want %v", got, want)
	}
}
