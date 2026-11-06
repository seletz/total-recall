package ops

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/ingest"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(config.Options{GOOS: "linux", Home: "/home/u", Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLockDir(t *testing.T) {
	cfg := testConfig(t)
	if got, want := LockDir(cfg), filepath.Join(filepath.Dir(cfg.DB), "locks"); got != want {
		t.Errorf("LockDir = %q, want %q", got, want)
	}
}

func TestMaintainOptions(t *testing.T) {
	cfg := testConfig(t)
	now := time.Date(2026, 11, 5, 23, 30, 0, 0, time.UTC)
	o := MaintainOptions(cfg, now, 0, true, time.Minute)
	if o.PageSize != RecommendedPageSize || !o.DryRun || o.Wait != time.Minute || o.LockDir != LockDir(cfg) ||
		o.BackupDir != cfg.Backup.Dir || o.KeepDaily != 7 || o.KeepMonthly != 0 {
		t.Errorf("options = %+v", o)
	}
	// Now is in the configured zone: the backup file is named by the local date.
	if o.Now.Location().String() != "Europe/Berlin" || o.Now.Day() != 6 {
		t.Errorf("Now = %v", o.Now)
	}
	if o := MaintainOptions(cfg, now, 4096, false, 0); o.PageSize != 4096 {
		t.Errorf("page size = %d", o.PageSize)
	}
}

func TestAdapters(t *testing.T) {
	cfg := testConfig(t)
	if got := Adapters(cfg); len(got) != 1 || got[0].Name() != "claude" {
		t.Errorf("Adapters = %v", got)
	}
	cfg.Harness.Claude.Enabled = false
	if got := Adapters(cfg); len(got) != 0 {
		t.Errorf("Adapters with claude disabled = %v", got)
	}
}

func TestClaudeHooks(t *testing.T) {
	b, err := json.Marshal(ClaudeHooks("/usr/local/bin/total-recall"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, ev := range HookEvents {
		if !strings.Contains(s, `"`+ev+`"`) {
			t.Errorf("event %s missing: %s", ev, s)
		}
	}
	if strings.Count(s, "/usr/local/bin/total-recall ingest --hook --detach") != 4 || strings.Count(s, "backup --if-older 24h --detach") != 1 {
		t.Errorf("commands: %s", s)
	}
}

func TestFormatIngestMemory(t *testing.T) {
	for _, c := range []struct {
		s    ingest.Summary
		want string
		not  string
	}{
		{s: ingest.Summary{Scanned: 1}, not: "memory"},
		{s: ingest.Summary{Scanned: 1, MemoryRead: 3}, not: "memory"},
		{s: ingest.Summary{Scanned: 1, MemoryVersions: 3, MemoryDeleted: 1}, want: ", memory: 3 new versions, 1 deleted"},
		{s: ingest.Summary{MemoryVersions: 2}, want: ", memory: 2 new versions, 0 deleted"},
	} {
		got := FormatIngest(c.s, false)
		if c.want != "" && !strings.Contains(got, c.want) || c.not != "" && strings.Contains(got, c.not) {
			t.Errorf("FormatIngest(%+v) = %q", c.s, got)
		}
	}
}
