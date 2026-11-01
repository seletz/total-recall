package skill

import (
	"strings"
	"testing"
)

func TestTextNotEmpty(t *testing.T) {
	if strings.TrimSpace(Text()) == "" {
		t.Fatal("Text() is empty")
	}
}

func TestTextHasFrontmatter(t *testing.T) {
	text := Text()
	if !strings.HasPrefix(text, "---\n") {
		t.Fatal("SKILL.md must start with YAML frontmatter")
	}
	for _, want := range []string{"name: total-recall\n", "description: "} {
		if !strings.Contains(text, want) {
			t.Errorf("frontmatter missing %q", want)
		}
	}
}

func TestTextMentionsKeyContent(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"sql command", "total-recall sql"},
		{"sessions command", "total-recall sessions"},
		{"search command", "total-recall search"},
		{"show command", "total-recall show"},
		{"stats command", "total-recall stats"},
		{"status command", "total-recall status"},
		{"json flag", "--json"},
		{"sessions table", "sessions"},
		{"events table", "events"},
		{"fts table", "events_fts"},
		{"exchanges view", "exchanges"},
		{"daily_activity view", "daily_activity"},
		{"utc caveat", "UTC"},
		{"redaction caveat", "redacted"},
	}
	text := Text()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(text, tt.want) {
				t.Errorf("SKILL.md does not mention %q", tt.want)
			}
		})
	}
}
