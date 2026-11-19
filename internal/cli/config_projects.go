package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/project"
)

// ConfigProjectsJSON is the JSON output of config projects. Every key is
// always present. Sessions are the main sessions with a working directory
// (the ones store.ProjectKeys counts, subagent sessions belong to their
// parent), and they add up: the Sessions of all Definitions plus
// Unassigned.Sessions are Totals.Sessions, as are the Sessions of all
// Keys; the same holds for Paths.
type ConfigProjectsJSON struct {
	// Definitions are the [[project]] entries in file order with what each
	// gets of the archive.
	Definitions []ProjectStatJSON `json:"definitions"`
	// Keys are the raw project keys of the archive (git root, else
	// directory), most sessions first, with the project they belong to.
	Keys []ProjectKeyJSON `json:"keys"`
	// Unassigned counts the keys no definition matches ("no project").
	Unassigned CountJSON `json:"unassigned"`
	Totals     CountJSON `json:"totals"`
	// Problem is the first error of the definition list, null without.
	// Definitions that do not validate are left out of the assignment.
	Problem NullString `json:"problem"`
	// Preview is set with --preview, null otherwise.
	Preview *ProjectPreviewJSON `json:"preview"`
}

// CountJSON counts paths (project keys) and their sessions.
type CountJSON struct {
	Paths    int `json:"paths"`
	Sessions int `json:"sessions"`
}

// ProjectStatJSON is a definition with the paths and sessions it gets.
type ProjectStatJSON struct {
	ProjectJSON
	Paths    int `json:"paths"`
	Sessions int `json:"sessions"`
}

// ProjectKeyJSON is one raw project key of the archive. Name is what the
// key is called without definitions, Project the definition it belongs to
// (null: no project). Dir is true for a working directory without a git
// repository; Remote and Last are null when unknown.
type ProjectKeyJSON struct {
	Key      string     `json:"key"`
	Name     string     `json:"name"`
	Dir      bool       `json:"dir"`
	Remote   NullString `json:"remote"`
	Sessions int        `json:"sessions"`
	Last     NullString `json:"last"`
	Project  NullString `json:"project"`
}

// ProjectPreviewJSON is what a draft definition would match, see --preview.
// Paths and Sessions count the keys the draft gets; MatchedSessions adds
// the sessions of keys its patterns match but an earlier definition takes
// (Matches lists both, with TakenBy set for the latter). Replaces is true
// when the draft has the name of an existing definition and stands in its
// place. Text is the one-line summary the projects page shows.
type ProjectPreviewJSON struct {
	Definition      ProjectJSON        `json:"definition"`
	Replaces        bool               `json:"replaces"`
	Paths           int                `json:"paths"`
	Sessions        int                `json:"sessions"`
	MatchedSessions int                `json:"matched_sessions"`
	Matches         []ProjectMatchJSON `json:"matches"`
	Text            string             `json:"text"`
}

// ProjectMatchJSON is one key a draft's patterns match.
type ProjectMatchJSON struct {
	Key      string     `json:"key"`
	Name     string     `json:"name"`
	Sessions int        `json:"sessions"`
	TakenBy  NullString `json:"taken_by"`
}

func (e *env) configProjectsCmd() *cobra.Command {
	var of outputFlags
	var f projectFlags
	var preview bool
	c := &cobra.Command{
		Use:   "projects",
		Short: "Project definitions with their sessions, the project keys of the archive, and a preview of a draft",
		Long: `List the [[project]] definitions with the paths and sessions each gets, and the
project keys of the archive (git root, else working directory) with the
definition they belong to, as the projects page of the TUI shows them. The
sessions add up: the definitions plus the keys without a definition are all
main sessions with a directory.

--preview checks a draft against the archive without saving it: --match
(repeatable) are its patterns, --name its name (a definition of that name is
replaced by the draft, otherwise it comes last), --description and --color
as in config project add.`,
		Args: usageArgs(cobra.NoArgs),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		fl := cmd.Flags()
		drafted := fl.Changed("name") || fl.Changed("match") || fl.Changed("description") || fl.Changed("color")
		if drafted && !preview {
			return usagef("--name, --match, --description and --color describe a draft: add --preview")
		}
		if preview && len(f.match) == 0 {
			return usagef("--preview needs at least one --match")
		}
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		st, err := e.openRead(cfg)
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
		keys, err := st.ProjectKeys(cmd.Context())
		if err != nil {
			return err
		}
		ar := project.NewArchive(keys, cfg.Home)
		r, perr := cfg.PageResolver()
		out := projectsReport(cfg, ar, r)
		if perr != nil {
			out.Problem = NullString(perr.Error())
		}
		if preview {
			p, err := projectPreview(cfg, ar, f)
			if err != nil {
				return errKind(ErrKindValidation, err)
			}
			out.Preview = &p
		}
		if o.format.IsJSON() {
			return o.object(out)
		}
		return writeProjectsTable(o, out)
	})
	fl := c.Flags()
	fl.BoolVar(&preview, "preview", false, "check a draft definition against the archive (nothing is saved)")
	fl.StringVar(&f.name, "name", "", "draft: name (an existing definition of that name is replaced)")
	fl.StringArrayVar(&f.match, "match", nil, "draft: pattern; repeat for several")
	fl.StringVar(&f.description, "description", "", "draft: description")
	fl.StringVar(&f.color, "color", "", "draft: palette name or #rrggbb")
	of.register(c)
	return c
}

// projectsReport assigns the keys of the archive under r.
func projectsReport(cfg *config.Config, ar project.Archive, r project.Resolver) ConfigProjectsJSON {
	as := ar.Assign(r)
	out := ConfigProjectsJSON{Definitions: []ProjectStatJSON{}, Keys: make([]ProjectKeyJSON, 0, len(ar.Keys))}
	owner := map[int]string{}
	for name, idx := range as.ByName {
		for _, i := range idx {
			owner[i] = name
		}
	}
	for i, k := range ar.Keys {
		out.Keys = append(out.Keys, ProjectKeyJSON{Key: k.Key, Name: ar.Name(i), Dir: k.Dir, Remote: NullString(k.Remote), Sessions: k.Sessions, Last: NullString(isoTime(k.Last)), Project: NullString(owner[i])})
	}
	for i, p := range cfg.Projects {
		paths, sessions := ar.Tally(as.ByName[p.Name])
		out.Definitions = append(out.Definitions, ProjectStatJSON{ProjectJSON: projectJSON(i, p), Paths: paths, Sessions: sessions})
	}
	out.Unassigned.Paths, out.Unassigned.Sessions = ar.Tally(as.Unmatched)
	out.Totals.Paths, out.Totals.Sessions = ar.Tally(allIndexes(len(ar.Keys)))
	return out
}

func allIndexes(n int) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	return idx
}

// projectPreview checks the draft of f against the archive.
func projectPreview(cfg *config.Config, ar project.Archive, f projectFlags) (ProjectPreviewJSON, error) {
	draft := config.Project{Name: f.name, Description: f.description, Color: f.color, Match: f.match}
	if draft.Name == "" {
		draft.Name = "draft"
	}
	index := -1
	for i, p := range cfg.Projects {
		if p.Name == draft.Name {
			index = i
		}
	}
	r, err := cfg.DraftResolver(index, draft)
	if err != nil {
		return ProjectPreviewJSON{}, err
	}
	hs, err := ar.Hits(r, draft.Name, draft.Match, cfg.Home)
	if err != nil {
		return ProjectPreviewJSON{}, err
	}
	pos := index
	if pos < 0 {
		pos = len(cfg.Projects)
	}
	out := ProjectPreviewJSON{Definition: projectJSON(pos, draft), Replaces: index >= 0, Matches: []ProjectMatchJSON{}, Text: ar.Describe(hs)}
	for _, h := range hs {
		k := ar.Keys[h.Key]
		out.Matches = append(out.Matches, ProjectMatchJSON{Key: k.Key, Name: ar.Name(h.Key), Sessions: k.Sessions, TakenBy: NullString(h.Taken)})
		out.MatchedSessions += k.Sessions
		if h.Taken == "" {
			out.Paths++
			out.Sessions += k.Sessions
		}
	}
	return out, nil
}

func writeProjectsTable(o *output, c ConfigProjectsJSON) error {
	rows := make([][]string, 0, len(c.Definitions))
	for _, d := range c.Definitions {
		rows = append(rows, []string{d.Name, fmt.Sprint(d.Sessions), fmt.Sprint(d.Paths), strings.Join(d.Match, ", ")})
	}
	if len(rows) > 0 {
		if err := o.table([]string{"PROJECT", "SESSIONS", "PATHS", "MATCH"}, rows); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(o.w)
	}
	krows := make([][]string, 0, len(c.Keys))
	for _, k := range c.Keys {
		p := string(k.Project)
		if p == "" {
			p = "-"
		}
		krows = append(krows, []string{k.Key, fmt.Sprint(k.Sessions), p})
	}
	if err := o.table([]string{"PATH", "SESSIONS", "PROJECT"}, krows); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(o.w, "\n%d sessions in %d paths; %d sessions in %d paths without a project\n", c.Totals.Sessions, c.Totals.Paths, c.Unassigned.Sessions, c.Unassigned.Paths)
	if c.Problem != "" {
		_, _ = fmt.Fprintf(o.w, "problem: %s\n", c.Problem)
	}
	if p := c.Preview; p != nil {
		_, _ = fmt.Fprintf(o.w, "preview %s: %s\n", p.Definition.Name, p.Text)
	}
	return nil
}
