package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
)

// ProjectJSON is one [[project]] definition in JSON output. Position is
// the place in the file, starting at 1: the first definition that matches
// wins. Description and Color are null when the definition has none (the
// colour is then picked automatically); Match is never null.
type ProjectJSON struct {
	Position    int        `json:"position"`
	Name        string     `json:"name"`
	Description NullString `json:"description"`
	Color       NullString `json:"color"`
	Match       []string   `json:"match"`
}

// projectsJSON lists the definitions of cfg in file order.
func projectsJSON(cfg *config.Config) []ProjectJSON {
	out := make([]ProjectJSON, 0, len(cfg.Projects))
	for i, p := range cfg.Projects {
		out = append(out, projectJSON(i, p))
	}
	return out
}

func projectJSON(index int, p config.Project) ProjectJSON {
	m := append([]string{}, p.Match...)
	return ProjectJSON{Position: index + 1, Name: p.Name, Description: NullString(p.Description), Color: NullString(p.Color), Match: m}
}

// ConfigProjectJSON is the JSON output of config project add|set|rm|move.
// Project is the definition after the change (for rm: the one removed),
// Projects all definitions afterwards. Changed is false when the file
// already was as asked (a move to the place it is at). Every key is always
// present.
type ConfigProjectJSON struct {
	File     string        `json:"file"`
	Action   string        `json:"action"`
	Changed  bool          `json:"changed"`
	Project  ProjectJSON   `json:"project"`
	Projects []ProjectJSON `json:"projects"`
}

func (e *env) configProjectCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "project",
		Short: "Add, change, remove or move project definitions ([[project]]) in the config file",
		Long: `Edit the [[project]] definitions like the projects page of the TUI: the
same functions write the file, comments and the order of the file are kept, the
previous file is kept as <file>.bak, and a definition that does not validate
(no name, no pattern, unknown colour, a name twice) is refused with the error
kind validation. Definitions are addressed by name. Only the config file
changes, never the archive.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return usage(cmd.Help()) },
	}
	c.AddCommand(e.projectAddCmd(), e.projectSetCmd(), e.projectRmCmd(), e.projectMoveCmd())
	return c
}

// projectFlags are the fields of a definition on add and set.
type projectFlags struct {
	name, description, color string
	match                    []string
}

func (f *projectFlags) register(c *cobra.Command, withName bool) {
	fl := c.Flags()
	if withName {
		fl.StringVar(&f.name, "name", "", "new name")
	}
	fl.StringArrayVar(&f.match, "match", nil, "pattern (glob over the path, \"~\", or a remote URL); repeat for several")
	fl.StringVar(&f.description, "description", "", "description")
	fl.StringVar(&f.color, "color", "", "palette name or #rrggbb; empty = automatic")
}

// projectRun is the shared frame of the four commands: load the
// configuration, call do, report in the output format.
func (e *env) projectRun(of *outputFlags, action string, do func(cmd *cobra.Command, args []string, cfg *config.Config) (n *config.Config, shown config.Project, shownIndex int, err error)) func(*cobra.Command, []string) error {
	return e.readRun(of, func(cmd *cobra.Command, args []string, o *output) error {
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		n, shown, idx, err := do(cmd, args, cfg)
		if err != nil {
			if config.IsInvalid(err) {
				return errKind(ErrKindValidation, err)
			}
			return err
		}
		out := ConfigProjectJSON{File: n.Path, Action: action, Project: projectJSON(idx, shown), Projects: projectsJSON(n)}
		out.Changed = !sameProjects(cfg.Projects, n.Projects)
		if o.format.IsJSON() {
			return o.object(out)
		}
		rows := make([][]string, 0, len(out.Projects))
		for _, p := range out.Projects {
			rows = append(rows, []string{fmt.Sprint(p.Position), p.Name, string(p.Color), strings.Join(p.Match, ", "), string(p.Description)})
		}
		if err := o.table([]string{"#", "NAME", "COLOR", "MATCH", "DESCRIPTION"}, rows); err != nil {
			return err
		}
		return nil
	})
}

func sameProjects(a, b []config.Project) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Name != y.Name || x.Description != y.Description || x.Color != y.Color || strings.Join(x.Match, "\x00") != strings.Join(y.Match, "\x00") {
			return false
		}
	}
	return true
}

// findProject returns the index of the definition called name.
func findProject(cfg *config.Config, name string) (int, error) {
	for i, p := range cfg.Projects {
		if p.Name == name {
			return i, nil
		}
	}
	return -1, errKind(ErrKindNotFound, fmt.Errorf("no project %q in the config (config project add)", name))
}

func (e *env) projectAddCmd() *cobra.Command {
	var of outputFlags
	var f projectFlags
	c := &cobra.Command{
		Use:   "add NAME --match PATTERN [--match …] [--description TEXT] [--color COLOR]",
		Short: "Append a project definition",
		Args:  usageArgs(cobra.ExactArgs(1)),
	}
	c.RunE = e.projectRun(&of, "add", func(_ *cobra.Command, args []string, cfg *config.Config) (*config.Config, config.Project, int, error) {
		p := config.Project{Name: args[0], Description: f.description, Color: f.color, Match: f.match}
		n, err := cfg.AddProject(p)
		return n, p, len(cfg.Projects), err
	})
	f.register(c, false)
	of.register(c)
	return c
}

func (e *env) projectSetCmd() *cobra.Command {
	var of outputFlags
	var f projectFlags
	c := &cobra.Command{
		Use:   "set NAME [--name NEW] [--match PATTERN …] [--description TEXT] [--color COLOR]",
		Short: "Change fields of a project definition (only the flags given; \"\" clears description and colour)",
		Long: `Change the fields named by a flag and keep the others. --match replaces all
patterns. An empty --description or --color removes it.`,
		Args: usageArgs(cobra.ExactArgs(1)),
	}
	c.RunE = e.projectRun(&of, "set", func(cmd *cobra.Command, args []string, cfg *config.Config) (*config.Config, config.Project, int, error) {
		fl := cmd.Flags()
		if !fl.Changed("name") && !fl.Changed("match") && !fl.Changed("description") && !fl.Changed("color") {
			return nil, config.Project{}, 0, usagef("set: give at least one of --name, --match, --description, --color")
		}
		i, err := findProject(cfg, args[0])
		if err != nil {
			return nil, config.Project{}, 0, err
		}
		p := cfg.Projects[i]
		if fl.Changed("name") {
			p.Name = f.name
		}
		if fl.Changed("match") {
			p.Match = f.match
		}
		if fl.Changed("description") {
			p.Description = f.description
		}
		if fl.Changed("color") {
			p.Color = f.color
		}
		n, err := cfg.SetProject(i, p)
		return n, p, i, err
	})
	f.register(c, true)
	of.register(c)
	return c
}

func (e *env) projectRmCmd() *cobra.Command {
	var of outputFlags
	c := &cobra.Command{
		Use:   "rm NAME",
		Short: "Remove a project definition",
		Args:  usageArgs(cobra.ExactArgs(1)),
	}
	c.RunE = e.projectRun(&of, "rm", func(_ *cobra.Command, args []string, cfg *config.Config) (*config.Config, config.Project, int, error) {
		i, err := findProject(cfg, args[0])
		if err != nil {
			return nil, config.Project{}, 0, err
		}
		n, err := cfg.RemoveProject(i)
		return n, cfg.Projects[i], i, err
	})
	of.register(c)
	return c
}

func (e *env) projectMoveCmd() *cobra.Command {
	var of outputFlags
	var to int
	c := &cobra.Command{
		Use:   "move NAME --to POSITION",
		Short: "Move a project definition to a position (1 = first; the first match wins)",
		Args:  usageArgs(cobra.ExactArgs(1)),
	}
	c.RunE = e.projectRun(&of, "move", func(cmd *cobra.Command, args []string, cfg *config.Config) (*config.Config, config.Project, int, error) {
		if !cmd.Flags().Changed("to") {
			return nil, config.Project{}, 0, usagef("move: --to POSITION is required")
		}
		i, err := findProject(cfg, args[0])
		if err != nil {
			return nil, config.Project{}, 0, err
		}
		n, err := cfg.MoveProject(i, to-1)
		return n, cfg.Projects[i], to - 1, err
	})
	c.Flags().IntVar(&to, "to", 0, "new position, 1 = first")
	of.register(c)
	return c
}
