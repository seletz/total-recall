package config

import (
	"os"
	"strings"
	"testing"
)

// projectNames lists the names of the configured projects in order.
func projectNames(c *Config) string {
	names := make([]string, len(c.Projects))
	for i, p := range c.Projects {
		names[i] = p.Name
	}
	return strings.Join(names, ",")
}

func TestProjectsEdit(t *testing.T) {
	c := editEnv(t, Template, nil)
	n, err := c.AddProject(Project{Name: "kompass", Description: "Wanderkarten", Color: "blue", Match: []string{"~/src/kompass*", "https://git.invalid/mara/kompass"}})
	if err != nil {
		t.Fatal(err)
	}
	n, err = n.AddProject(Project{Name: "notizbox", Match: []string{"~/notizen"}})
	if err != nil {
		t.Fatal(err)
	}
	if projectNames(n) != "kompass,notizbox" || n.Projects[0].Color != "blue" || n.Projects[0].Description != "Wanderkarten" || len(n.Projects[0].Match) != 2 {
		t.Errorf("projects = %+v", n.Projects)
	}
	b, _ := os.ReadFile(c.Path)
	for _, w := range []string{
		"\n[[project]]\nname = \"kompass\"\ndescription = \"Wanderkarten\"\ncolor = \"blue\"\nmatch = [\"~/src/kompass*\", \"https://git.invalid/mara/kompass\"]\n",
		"\n[[project]]\nname = \"notizbox\"\nmatch = \"~/notizen\"\n",
		"# [[project]]", // the commented template block stays
	} {
		if !strings.Contains(string(b), w) {
			t.Errorf("file lacks %q:\n%s", w, b)
		}
	}
	if _, err := os.Stat(c.Path + ".bak"); err != nil {
		t.Errorf("no backup: %v", err)
	}

	// Change the second: name, a description, a colour; then clear them.
	n, err = n.SetProject(1, Project{Name: "notizen", Description: "Markdown", Color: "#112233", Match: []string{"~/notizen", "~/notes"}})
	if err != nil {
		t.Fatal(err)
	}
	if p := n.Projects[1]; p.Name != "notizen" || p.Description != "Markdown" || p.Color != "#112233" || len(p.Match) != 2 {
		t.Errorf("project[1] = %+v", p)
	}
	n, err = n.SetProject(1, Project{Name: "notizen", Match: []string{"~/notizen"}})
	if err != nil {
		t.Fatal(err)
	}
	if p := n.Projects[1]; p.Description != "" || p.Color != "" || len(p.Match) != 1 {
		t.Errorf("project[1] after clearing = %+v", p)
	}
	b, _ = os.ReadFile(c.Path)
	for _, w := range []string{"\nname = \"notizen\"\n", "\n# description = \"Markdown\"\n", "\n# color = \"#112233\"\n", "\nmatch = \"~/notizen\"\n"} {
		if !strings.Contains(string(b), w) {
			t.Errorf("file lacks %q:\n%s", w, b)
		}
	}

	// Move and remove.
	n, err = n.MoveProject(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if projectNames(n) != "notizen,kompass" {
		t.Errorf("after move: %s", projectNames(n))
	}
	n, err = n.RemoveProject(0)
	if err != nil {
		t.Fatal(err)
	}
	if projectNames(n) != "kompass" {
		t.Errorf("after remove: %s", projectNames(n))
	}
	b, _ = os.ReadFile(c.Path)
	if strings.Contains(string(b), "notizen") {
		t.Errorf("removed project still in the file:\n%s", b)
	}
}

func TestProjectsEditRejects(t *testing.T) {
	c := editEnv(t, Template+"\n[[project]]\nname = \"kompass\"\nmatch = \"~/src/kompass\"\n", nil)
	before, _ := os.ReadFile(c.Path)
	tests := []struct {
		name string
		edit func() (*Config, error)
		want string
	}{
		{"no name", func() (*Config, error) { return c.AddProject(Project{Match: []string{"a"}}) }, "without a name"},
		{"no pattern", func() (*Config, error) { return c.AddProject(Project{Name: "x"}) }, "no match patterns"},
		{"empty pattern", func() (*Config, error) { return c.AddProject(Project{Name: "x", Match: []string{""}}) }, "empty match pattern"},
		{"bad colour", func() (*Config, error) { return c.AddProject(Project{Name: "x", Color: "mauve", Match: []string{"a"}}) }, "color"},
		{"duplicate", func() (*Config, error) { return c.AddProject(Project{Name: "kompass", Match: []string{"a"}}) }, "listed twice"},
		{"set out of range", func() (*Config, error) { return c.SetProject(3, Project{Name: "x", Match: []string{"a"}}) }, "no index 3"},
		{"remove out of range", func() (*Config, error) { return c.RemoveProject(-1) }, "no index -1"},
		{"move out of range", func() (*Config, error) { return c.MoveProject(0, 1) }, "no move 0 -> 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.edit()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			after, _ := os.ReadFile(c.Path)
			if string(after) != string(before) {
				t.Errorf("file changed:\n%s", after)
			}
		})
	}
}
