package store

import (
	"errors"
	"path/filepath"
	"regexp"
	"testing"
)

var archiveIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// TestArchiveIdentity: every archive has an ID of its own from the moment
// it has schema v11; WithHost records the host on a read-write open and
// changes it when the configuration changes; a read-only store reads both.
func TestArchiveIdentity(t *testing.T) {
	s, p := openTestStore(t)
	a, err := s.Archive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !archiveIDPattern.MatchString(a.ID) || a.Host != "" {
		t.Fatalf("new archive = %+v, want an ID and no host", a)
	}
	_, p2 := openTestStore(t)
	other, err := openArchive(t, p2)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == a.ID {
		t.Errorf("two archives share the ID %s", a.ID)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		host, want string
	}{
		{"host-a", "host-a"},
		{"host-a", "host-a"}, // unchanged: no write
		{"host-b", "host-b"}, // the configuration changed
		{"", "host-b"},       // no host configured: the archive keeps its own
	} {
		s, err := Open(p, WithHost(tc.host))
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.Archive(ctx)
		_ = s.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got.Host != tc.want || got.ID != a.ID {
			t.Errorf("WithHost(%q): %+v, want host %q and ID %s", tc.host, got, tc.want, a.ID)
		}
	}
	ro, err := OpenReadOnly(p, WithHost("ignored"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	if got, err := ro.Archive(ctx); err != nil || got.Host != "host-b" || got.ID != a.ID {
		t.Errorf("read-only: %+v (%v)", got, err)
	}
}

// TestArchiveIdentityOutdated: an archive before v11 has no identity yet;
// Archive reports that without an error, and WithHost writes nothing
// into an archive opened with AllowOutdated (backup before a migration).
func TestArchiveIdentityOutdated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "archive.db")
	if err := CreateAtVersion(p, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p, WithHost("host-a")); !errors.Is(err, ErrSchemaOutdated) {
		t.Fatalf("Open v10 = %v, want ErrSchemaOutdated", err)
	}
	s, err := Open(p, WithHost("host-a"), AllowOutdated())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if a, err := s.Archive(ctx); err != nil || a != (Archive{}) {
		t.Errorf("Archive before v11 = %+v (%v), want zero", a, err)
	}
	if n := countRows(t, s, "SELECT count(*) FROM sqlite_master WHERE name = 'archive_meta'"); n != 0 {
		t.Errorf("archive_meta exists before the migration")
	}
}

// TestMigrateRecordsHost: Migrate gives the archive its ID, and with
// MigrateOptions.Host records the host right away, so the next status shows it.
func TestMigrateRecordsHost(t *testing.T) {
	p := filepath.Join(t.TempDir(), "archive.db")
	if err := CreateAtVersion(p, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, p, MigrateOptions{LockDir: filepath.Join(t.TempDir(), "locks"), Host: "host-a"}); err != nil {
		t.Fatal(err)
	}
	a, err := openArchive(t, p)
	if err != nil {
		t.Fatal(err)
	}
	if !archiveIDPattern.MatchString(a.ID) || a.Host != "host-a" {
		t.Errorf("after migrate: %+v", a)
	}
}

func openArchive(t *testing.T, p string) (Archive, error) {
	t.Helper()
	s, err := OpenReadOnly(p)
	if err != nil {
		return Archive{}, err
	}
	defer func() { _ = s.Close() }()
	return s.Archive(ctx)
}
