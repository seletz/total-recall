package cli

import (
	"io"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
)

// ToolCountJSON is the number of tool_use events of one tool.
type ToolCountJSON struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
}

// ToolGroupJSON is the tool use of one group.
type ToolGroupJSON struct {
	Key    string          `json:"key"`
	Count  int             `json:"count"`
	ByTool []ToolCountJSON `json:"by_tool"`
}

// ToolStatsJSON is the --json output of stats --tools. Every key is always
// present. Total is the number of tool_use events in the range (matching
// Path); ByTool sums to Total and, with --by, so do the Groups' counts
// (--top folds the rest into the group "other", nothing is dropped).
type ToolStatsJSON struct {
	Since    NullString `json:"since"`
	Until    NullString `json:"until"`
	Timezone string     `json:"timezone"`
	// By is day, session, repo, project or null.
	By NullString `json:"by"`
	// Path is the --path filter or null.
	Path   NullString      `json:"path"`
	Total  int             `json:"total"`
	ByTool []ToolCountJSON `json:"by_tool"`
	// Groups is empty without --by. Per day they run chronologically,
	// otherwise by count descending with "other" last. For --by project
	// the keys are the project names, like stats --by project.
	Groups []ToolGroupJSON `json:"groups"`
}

func toolCounts(c []store.ToolCount) []ToolCountJSON {
	out := make([]ToolCountJSON, 0, len(c))
	for _, x := range c {
		out = append(out, ToolCountJSON{Tool: x.Tool, Count: x.Count})
	}
	return out
}

// toolStatsJSON converts the result; names maps raw group keys to display
// names (project dimension), keys without an entry are shown as they are.
func toolStatsJSON(s store.ToolStats, f store.ToolFilter, names map[string]string, since, until time.Time, tz string) ToolStatsJSON {
	out := ToolStatsJSON{
		Since: NullString(isoTime(since)), Until: NullString(isoTime(until)), Timezone: tz,
		By: NullString(f.By.String()), Path: NullString(f.Path),
		Total: s.Total, ByTool: toolCounts(s.ByTool), Groups: make([]ToolGroupJSON, 0, len(s.Groups)),
	}
	for _, g := range s.Groups {
		key := g.Key
		if n, ok := names[key]; ok {
			key = n
		}
		out.Groups = append(out.Groups, ToolGroupJSON{Key: key, Count: g.Count, ByTool: toolCounts(g.ByTool)})
	}
	return out
}

// writeToolStatsTable writes TOOL COUNT without --by, else KEY TOOL COUNT
// per group and tool; the rows labelled TotalLabel close the table (per tool
// and, with --by, TotalKey for all tools).
func writeToolStatsTable(w io.Writer, s ToolStatsJSON) error {
	n := strconv.Itoa
	if s.By == "" {
		rows := make([][]string, 0, len(s.ByTool)+1)
		for _, c := range s.ByTool {
			rows = append(rows, []string{c.Tool, n(c.Count)})
		}
		rows = append(rows, []string{TotalLabel, n(s.Total)})
		return table(w, []string{"TOOL", "COUNT"}, rows)
	}
	var rows [][]string
	for _, g := range s.Groups {
		for _, c := range g.ByTool {
			rows = append(rows, []string{g.Key, c.Tool, n(c.Count)})
		}
	}
	for _, c := range s.ByTool {
		rows = append(rows, []string{TotalLabel, c.Tool, n(c.Count)})
	}
	rows = append(rows, []string{TotalLabel, TotalKey, n(s.Total)})
	return table(w, []string{"KEY", "TOOL", "COUNT"}, rows)
}

// runToolStats is stats --tools: by is the --by value ("" without), path
// the --path glob, top the --top count.
func (e *env) runToolStats(cmd *cobra.Command, cfg *config.Config, o *output, since, until time.Time, by, path string, top int) error {
	f := store.ToolFilter{Since: since, Until: until, Location: cfg.Location(), Path: path, TopN: top}
	if by != "" {
		g, err := store.ParseToolGroup(by)
		if err != nil {
			return usagef("%v", err)
		}
		f.By = g
	}
	switch {
	case top < 0:
		return usagef("--top must not be negative")
	case top > 0 && by == "":
		return usagef("--top requires --by")
	}
	var projects project.Resolver
	if f.By == store.ToolByProject {
		var err error
		if projects, err = cfg.ProjectResolver(); err != nil {
			return errKind(ErrKindConfig, err)
		}
		f.Project = projects.Key
	}
	st, err := e.openRead(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	s, err := st.ToolStats(cmd.Context(), f)
	if err != nil {
		return err
	}
	names := func(home string) map[string]string {
		if f.By != store.ToolByProject {
			return nil
		}
		keys := make([]string, 0, len(s.Groups))
		for _, g := range s.Groups {
			keys = append(keys, g.Key)
		}
		return projects.Names(keys, s.Dirs, home)
	}
	if o.format.IsJSON() {
		return o.object(toolStatsJSON(s, f, names(""), since, until, cfg.Timezone))
	}
	return writeToolStatsTable(o.w, toolStatsJSON(s, f, names(cfg.Home), since, until, cfg.Timezone))
}
