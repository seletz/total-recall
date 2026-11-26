package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorSetupJSON(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	claude := filepath.Join(e.dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	hook := `[{"hooks":[{"type":"command","command":"total-recall ingest --hook --detach"}]}]`
	settings := `{"hooks":{"SessionStart":` + hook + `,"Stop":` + hook + `}}`
	if err := os.WriteFile(filepath.Join(claude, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	before := e.countFiles(t)

	out := e.mustRun(t, "doctor", "--setup", "--json")
	raw := decode[map[string]any](t, out)
	for _, k := range []string{"ok", "checks"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("key %q missing", k)
		}
	}
	r := decode[DoctorSetupJSON](t, out)
	var names []string
	by := map[string]SetupCheckJSON{}
	for _, c := range r.Checks {
		names = append(names, c.Name)
		by[c.Name] = c
	}
	if strings.Join(names, ",") != "config,hooks,mcp,binary,backups,transcripts,archive" {
		t.Fatalf("checks = %v", names)
	}
	if !by["config"].OK || !strings.Contains(by["config"].Text, "loaded") {
		t.Errorf("config = %+v", by["config"])
	}
	// The same text the config page shows, with "~" for the home directory.
	if h := by["hooks"]; h.OK || h.Text != "~/.claude/settings.json lacks SubagentStop, SessionEnd; add the hook block (c copies it)" {
		t.Errorf("hooks = %+v", h)
	}
	if b := by["backups"]; b.OK || b.Fix != "create the directory" {
		t.Errorf("backups = %+v", b)
	}
	if by["config"].Fix != "" || by["mcp"].OK {
		t.Errorf("config/mcp = %+v / %+v", by["config"], by["mcp"])
	}
	if r.OK {
		t.Error("ok although checks fail")
	}
	// Reading only: no file was created, nor was the backup directory.
	if after := e.countFiles(t); after != before {
		t.Errorf("files before %d, after %d", before, after)
	}
	if _, err := os.Stat(e.backups); err == nil {
		t.Error("doctor --setup created the backup directory")
	}

	// Every key of a check is always present.
	for _, c := range raw["checks"].([]any) {
		c := c.(map[string]any)
		for _, k := range []string{"name", "ok", "warn", "text", "fix"} {
			if _, ok := c[k]; !ok {
				t.Errorf("check %v lacks %q", c["name"], k)
			}
		}
	}
}

func TestDoctorSetupFlagsAndTable(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	out := e.mustRun(t, "doctor", "--setup")
	for _, w := range []string{"hooks", "mcp", "binary", "backups", "transcripts", "archive"} {
		if !strings.Contains(out, w) {
			t.Errorf("table lacks %q:\n%s", w, out)
		}
	}
	for _, args := range [][]string{{"doctor", "--setup", "--harness", "claude", "--json"}, {"doctor", "--setup", "--days", "3", "--json"}} {
		r := e.run(t, "", args...)
		if r.code != 1 || decode[ErrorJSON](t, r.stdout).Error.Kind != ErrKindUsage {
			t.Errorf("%v = %+v", args, r)
		}
	}
}

// countFiles counts the files below the test installation.
func (e *testEnv) countFiles(t *testing.T) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(e.dir, func(_ string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}
