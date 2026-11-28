package scripts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// envOnly is what goreleaser accepts for a repository token
// (tmpl.ApplySingleEnvOnly): one environment variable, nothing else. A
// snapshot never pushes, so only a release would find a token that breaks it.
var envOnly = regexp.MustCompile(`^{{\s*\.Env\.[^.\s}]+\s*}}$`)

// values returns the unquoted values of every "key:" line of a YAML file.
func values(t *testing.T, file, key string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", file))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		v, ok := strings.CutPrefix(strings.TrimSpace(line), key+":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out = append(out, v)
	}
	return out
}

// TestGoreleaserTemplatesAreEnvOnly: the tap tokens of both configs, and
// the cask switch of the analytics config, are plain env templates; the
// workflows decide.
func TestGoreleaserTemplatesAreEnvOnly(t *testing.T) {
	for _, tc := range []struct{ file, key string }{
		{".goreleaser.yml", "token"},
		{".goreleaser-analytics.yml", "token"},
		{".goreleaser-analytics.yml", "skip_upload"},
	} {
		vs := values(t, tc.file, tc.key)
		if len(vs) == 0 {
			t.Errorf("%s: no %s", tc.file, tc.key)
		}
		for _, v := range vs {
			if !envOnly.MatchString(v) {
				t.Errorf("%s: %s %q, want {{ .Env.NAME }} only", tc.file, tc.key, v)
			}
		}
	}
}

// TestAnalyticsRunsSetSkipCask: every place that runs the analytics config
// sets ANALYTICS_SKIP_CASK, which its skip_upload reads (a missing variable
// fails the template).
func TestAnalyticsRunsSetSkipCask(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, filepath.Join("..", "mise.toml"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		runs := strings.Count(s, "--config .goreleaser-analytics.yml")
		sets := strings.Count(s, "ANALYTICS_SKIP_CASK")
		if runs != sets {
			t.Errorf("%s: %d runs of .goreleaser-analytics.yml, %d settings of ANALYTICS_SKIP_CASK", f, runs, sets)
		}
	}
}
