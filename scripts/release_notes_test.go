package scripts

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runReleaseNotes runs release-notes.sh for tag against the changelog file
// and returns stdout, stderr and the exit code.
func runReleaseNotes(t *testing.T, tag, file string) (string, string, int) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(".", "release-notes.sh"), tag, file)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run release-notes.sh: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

func TestReleaseNotes(t *testing.T) {
	const site = "https://seletz.github.io/total-recall/"
	fixture := filepath.Join("testdata", "changelog.md")

	tests := []struct {
		name       string
		tag        string
		wantStdout string
		wantStderr string // substring of stderr; empty means exit 0
	}{
		{
			name: "section with rewritten keys and links",
			tag:  "v0.2.0",
			wantStdout: "\n" +
				"Press `ctrl+c` to stop. See [commands](" + site + "reference/10-befehle/#ingest), " +
				"[design](" + site + "explanation/design/#raw), " +
				"[fixed](" + site + "reference/90-aenderungen/#behoben) and " +
				"[schema](" + site + "reference/40-schema/).\n" +
				"\n" +
				"### Behoben\n" +
				"\n" +
				"- A bug ([#12](https://github.com/example/repo/issues/12)).\n",
		},
		{
			name:       "last section runs to the end of the file",
			tag:        "v0.1.0",
			wantStdout: "\n- First release.\n",
		},
		{
			name:       "missing version fails",
			tag:        "v9.9.9",
			wantStderr: `no section "## v9.9.9"`,
		},
		{
			name:       "unreleased section fails",
			tag:        "v0.3.0",
			wantStderr: `"## v0.3.0 (unveröffentlicht)"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runReleaseNotes(t, tt.tag, fixture)
			if tt.wantStderr == "" {
				if code != 0 {
					t.Fatalf("exit %d, stderr: %s", code, stderr)
				}
				if stdout != tt.wantStdout {
					t.Errorf("stdout:\n%s\nwant:\n%s", stdout, tt.wantStdout)
				}
				return
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1; stdout: %q", code, stdout)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.wantStderr)
			}
		})
	}
}

func TestReleaseNotesDefaultFile(t *testing.T) {
	// Without a file argument the script reads the changelog of the repo; the
	// intro before the first section must not count as notes of a tag.
	if _, err := os.Stat(filepath.Join("..", "docs", "reference", "90-aenderungen.md")); err != nil {
		t.Skip("changelog not found:", err)
	}
	cmd := exec.Command("bash", filepath.Join(".", "release-notes.sh"), "v0.0.0-no-such-tag")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected a failure for an unknown tag, got output:\n%s", out)
	}
}
