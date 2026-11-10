package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// TestSyncDomain: the directory that makes a backup directory part of a
// file-provider (iCloud) domain: the directory itself or the nearest parent
// with the marker, nothing otherwise. The detection is injected.
func TestSyncDomain(t *testing.T) {
	marked := func(paths ...string) func(string) bool {
		return func(p string) bool {
			for _, m := range paths {
				if p == m {
					return true
				}
			}
			return false
		}
	}
	tests := []struct {
		name, dir string
		has       func(string) bool
		want      string
	}{
		{"the directory itself", "/h/Documents/total-recall", marked("/h/Documents/total-recall"), "/h/Documents/total-recall"},
		{"the parent", "/h/Documents/total-recall", marked("/h/Documents"), "/h/Documents"},
		{"a grandparent", "/h/Documents/a/b/c", marked("/h/Documents"), "/h/Documents"},
		{"the nearest one wins", "/h/Documents/a/b", marked("/h/Documents", "/h/Documents/a"), "/h/Documents/a"},
		{"a missing directory below a domain", "/h/Documents/not/there/yet", marked("/h/Documents"), "/h/Documents"},
		{"no domain", "/h/Library/Application Support/total-recall/backups", marked(), ""},
		{"a sibling does not count", "/h/Library/b", marked("/h/Documents"), ""},
		{"unclean path", "/h/Documents/../Library/b/", marked("/h/Documents"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := syncDomain(tt.dir, tt.has); got != filepath.FromSlash(tt.want) {
				t.Errorf("syncDomain = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDoctorBackupSyncHint: doctor tells when the backup directory is in a
// file-provider domain: in the text on stdout, with --json as the field
// backup_hint (null otherwise) and on stderr. It is no finding: the exit
// code stays 0 and the other fields stay.
func TestDoctorBackupSyncHint(t *testing.T) {
	_, db := fixturedb.Demo(t)
	tests := []struct {
		name     string
		domain   bool
		days     string
		wantHint bool
	}{
		{"in a domain", true, "30", true},
		{"in a domain, project hints off", true, "0", true},
		{"outside", false, "30", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			e.now = demo.Now
			e.home = demo.Home
			if tt.domain {
				e.fileProvider = func(p string) bool { return p == e.backups }
			}
			text := e.run(t, "", "--db", db, "doctor", "--days", tt.days)
			if text.code != 0 {
				t.Fatalf("exit %d\n%s%s", text.code, text.stdout, text.stderr)
			}
			if has := strings.Contains(text.stdout, "file provider"); has != tt.wantHint {
				t.Errorf("text hint = %v, want %v:\n%s", has, tt.wantHint, text.stdout)
			}
			if tt.wantHint && !strings.Contains(text.stdout, e.backups) {
				t.Errorf("hint does not name the directory: %s", text.stdout)
			}
			if tt.wantHint && !strings.Contains(text.stdout, "[backup] dir") {
				t.Errorf("hint does not say what to do: %s", text.stdout)
			}
			js := e.run(t, "", "--db", db, "doctor", "--json", "--days", tt.days)
			if js.code != 0 {
				t.Fatalf("exit %d\n%s%s", js.code, js.stdout, js.stderr)
			}
			if has := strings.Contains(js.stderr, "file provider"); has != tt.wantHint {
				t.Errorf("stderr hint = %v, want %v: %s", has, tt.wantHint, js.stderr)
			}
			var reports []map[string]json.RawMessage
			if err := json.Unmarshal([]byte(js.stdout), &reports); err != nil || len(reports) == 0 {
				t.Fatalf("stdout: %v\n%s", err, js.stdout)
			}
			raw, ok := reports[0]["backup_hint"]
			if !ok {
				t.Fatalf("no backup_hint key: %s", js.stdout)
			}
			if !tt.wantHint {
				if string(raw) != "null" {
					t.Errorf("backup_hint = %s, want null", raw)
				}
				return
			}
			var h struct{ Dir, DomainDir, Hint string }
			if err := json.Unmarshal(raw, &struct {
				Dir       *string `json:"dir"`
				DomainDir *string `json:"domain_dir"`
				Hint      *string `json:"hint"`
			}{&h.Dir, &h.DomainDir, &h.Hint}); err != nil {
				t.Fatal(err)
			}
			if h.Dir != e.backups || h.DomainDir != e.backups || !strings.Contains(h.Hint, "[backup] dir") {
				t.Errorf("backup_hint = %+v", h)
			}
			for _, k := range []string{"unknown_record_types", "hints", "versions"} {
				if _, ok := reports[0][k]; !ok {
					t.Errorf("key %q is gone", k)
				}
			}
		})
	}
}
