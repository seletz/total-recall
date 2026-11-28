package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestStatusHost: status shows the configured host and what the archive
// recorded; after a change of the key host the archive keeps the old name
// until the next write records the new one, and status says so.
func TestStatusHost(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	s := decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
	if s.Host != "host-a" || s.ArchiveHost != "host-a" || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(string(s.ArchiveID)) {
		t.Fatalf("status = host %q, archive host %q, id %q", s.Host, s.ArchiveHost, s.ArchiveID)
	}
	id := s.ArchiveID
	if out := e.mustRun(t, "status"); !strings.Contains(out, "host-a  archive "+string(id)) {
		t.Errorf("status lacks the host line:\n%s", out)
	}
	prependConfig(t, e, "host = \"host-b\"\n")
	s = decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
	if s.Host != "host-b" || s.ArchiveHost != "host-a" || s.ArchiveID != id {
		t.Errorf("after the config change = host %q, archive host %q, id %q", s.Host, s.ArchiveHost, s.ArchiveID)
	}
	if out := e.mustRun(t, "status"); !strings.Contains(out, "host-b  archive "+string(id)+", recorded as host-a until the next write") {
		t.Errorf("status lacks the pending host:\n%s", out)
	}
	e.mustRun(t, "ingest", "--all")
	s = decode[StatusJSON](t, e.mustRun(t, "status", "--json"))
	if s.Host != "host-b" || s.ArchiveHost != "host-b" || s.ArchiveID != id {
		t.Errorf("after the next write = host %q, archive host %q, id %q", s.Host, s.ArchiveHost, s.ArchiveID)
	}
}

// prependConfig puts content before the first table of the config, where
// top-level keys belong.
func prependConfig(t *testing.T, e *testEnv, content string) {
	t.Helper()
	cfg, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configPath, append([]byte(content), cfg...), 0o644); err != nil {
		t.Fatal(err)
	}
}
