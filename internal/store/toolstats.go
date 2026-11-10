package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ToolGroup is the grouping of ToolStats.
type ToolGroup int

// Groupings of ToolStats.
const (
	ToolNoGroup   ToolGroup = iota // no groups, only the totals per tool
	ToolByDay                      // local day, label 2006-01-02
	ToolBySession                  // session ID
	ToolByRepo                     // git remote URL, else git root, else cwd (like stats)
	ToolByProject                  // raw project key like DimProject; subagents count for their parent
)

// String returns day, session, repo or project ("" for ToolNoGroup).
func (g ToolGroup) String() string {
	switch g {
	case ToolByDay:
		return "day"
	case ToolBySession:
		return "session"
	case ToolByRepo:
		return "repo"
	case ToolByProject:
		return "project"
	case ToolNoGroup:
		return ""
	}
	return fmt.Sprintf("ToolGroup(%d)", int(g))
}

// ParseToolGroup returns the grouping named by s: day, session, repo or project.
func ParseToolGroup(s string) (ToolGroup, error) {
	for _, g := range []ToolGroup{ToolByDay, ToolBySession, ToolByRepo, ToolByProject} {
		if g.String() == s {
			return g, nil
		}
	}
	return ToolNoGroup, fmt.Errorf("invalid grouping %q for --tools (day, session, repo, project)", s)
}

// PathScope selects which tool calls ToolFilter.Path is checked against.
type PathScope int

// Scopes of ToolFilter.Path.
const (
	// PathScopePaths checks only calls of the tools in PathTools, whose
	// stored argument is a path, a path pattern or a command. The default.
	PathScopePaths PathScope = iota
	// PathScopeAll checks the stored argument of every tool call, also
	// messages to other sessions, prompts, URLs and queries.
	PathScopeAll
)

// String returns paths or all.
func (p PathScope) String() string {
	if p == PathScopeAll {
		return "all"
	}
	return "paths"
}

// ParsePathScope returns the scope named by s: paths or all.
func ParsePathScope(s string) (PathScope, error) {
	for _, p := range []PathScope{PathScopePaths, PathScopeAll} {
		if p.String() == s {
			return p, nil
		}
	}
	return PathScopePaths, fmt.Errorf("invalid path scope %q (paths, all)", s)
}

// PathTools are the tools whose argument is a path, a path pattern or a
// command, with the input fields that carry it. It is the one place that
// says which calls the default scope of ToolFilter.Path looks at; the
// command reference lists the same. The archive stores one text per call
// (events.text, the first present of command, file_path, path, ..., pattern),
// not the field names, so the tool name decides. A tool that is not listed
// (SendMessage, Task, WebFetch, WebSearch, Skill, MCP tools of any server,
// NotebookEdit whose path is not among the stored fields) is only found
// with PathScopeAll.
var PathTools = map[string][]string{
	"Bash":      {"command"},
	"Read":      {"file_path"},
	"Edit":      {"file_path"},
	"MultiEdit": {"file_path"},
	"Write":     {"file_path"},
	"Glob":      {"path", "pattern"},
	"Grep":      {"path", "pattern"},
	"LS":        {"path"},
}

// UnknownTool is the name under which tool_use events without a tool name
// are counted.
const UnknownTool = "unknown"

// ToolFilter selects the events and the grouping of ToolStats.
type ToolFilter struct {
	// Since/Until select events by time in [Since, Until); zero is open.
	Since, Until time.Time
	// Location is the time zone of the days of ToolByDay; default UTC.
	Location *time.Location
	By       ToolGroup
	// Path is an SQLite GLOB on the arguments of the call, which are the
	// stored input text of the tool_use event (the command of Bash, the
	// file_path or path of the file tools, else the first well-known field
	// or the compact JSON; cut at the adapter's tool-input limit). For
	// Bash the pattern may match anywhere in the command, for every other
	// tool it must match the whole argument. "" matches everything.
	Path string
	// PathIn selects the calls Path is checked against; see PathScope.
	// Ignored without Path.
	PathIn PathScope
	// TopN keeps the TopN largest groups and folds the rest into OtherKey
	// (last). 0 keeps all. Not applied to ToolByDay. Without a grouping
	// (ToolNoGroup) it keeps the TopN most used tools in ByTool and folds
	// the rest into one OtherKey entry (last); the sum stays Total.
	TopN int
	// Project renames the keys of ToolByProject, as ActivityFilter.Project.
	Project func(key string, dir bool, remote string) string
}

// ToolCount is the number of tool_use events of one tool.
type ToolCount struct {
	Tool  string
	Count int
}

// ToolGroupStats is the tool use of one group.
type ToolGroupStats struct {
	Key    string
	Count  int
	ByTool []ToolCount // largest first, then by name
}

// ToolStats counts tool_use events. Total equals the sum of ByTool, and
// with a grouping also the sum of Groups' counts: nothing is dropped.
type ToolStats struct {
	Total  int
	ByTool []ToolCount
	// Groups are sorted by day (ToolByDay) or by count descending, then
	// key; OtherKey is last. Empty with ToolNoGroup.
	Groups []ToolGroupStats
	// Dirs are the group keys of ToolByProject that are working directories
	// of sessions without a repository, like ActivitySeries.Dirs.
	Dirs []string
}

type toolTally map[string]int

func (t toolTally) count() int {
	n := 0
	for _, c := range t {
		n += c
	}
	return n
}

func (t toolTally) list() []ToolCount {
	out := make([]ToolCount, 0, len(t))
	for k, n := range t {
		out = append(out, ToolCount{k, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tool < out[j].Tool
	})
	return out
}

// ToolStats counts the tool_use events of f's range per tool and, with
// f.By, per group. Subagent sessions resolve to their parent for
// ToolByProject only.
func (s *Store) ToolStats(ctx context.Context, f ToolFilter) (ToolStats, error) {
	loc := f.Location
	if loc == nil {
		loc = time.UTC
	}
	cond := []string{"e.kind = 'tool_use'"}
	var args []any
	if !f.Since.IsZero() {
		cond = append(cond, "e.ts >= ?")
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		cond = append(cond, "e.ts < ?")
		args = append(args, formatTime(f.Until))
	}
	if f.Path != "" {
		cond = append(cond, "((e.tool_name = 'Bash' AND e.text GLOB '*' || ? || '*') OR (COALESCE(e.tool_name, '') <> 'Bash' AND e.text GLOB ?))")
		args = append(args, f.Path, f.Path)
		if f.PathIn == PathScopePaths {
			names := make([]string, 0, len(PathTools))
			for n := range PathTools {
				names = append(names, n)
			}
			sort.Strings(names)
			cond = append(cond, "e.tool_name IN ("+strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")+")")
			for _, n := range names {
				args = append(args, n)
			}
		}
	}
	rows, err := s.db().QueryContext(ctx, `SELECT e.ts, COALESCE(e.tool_name, ''), e.session_id, `+repoExpr+`,
			COALESCE(s.parent_id, ''), COALESCE(s.cwd, ''), COALESCE(s.git_root, ''),
			COALESCE(p.cwd, ''), COALESCE(p.git_root, ''),
			COALESCE(s.git_remote_url, ''), COALESCE(p.git_remote_url, '')
		FROM events e
		JOIN sessions s ON s.id = e.session_id
		LEFT JOIN sessions p ON p.id = s.parent_id
		WHERE `+strings.Join(cond, " AND "), args...)
	if err != nil {
		return ToolStats{}, fmt.Errorf("store: tool stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out ToolStats
	total := toolTally{}
	groups := map[string]toolTally{}
	isDir := map[string]bool{}
	for rows.Next() {
		var tsv sql.NullString
		var tool, session, repo, parentID, cwd, gitRoot, pcwd, pgitRoot, remote, premote string
		if err := rows.Scan(&tsv, &tool, &session, &repo, &parentID, &cwd, &gitRoot, &pcwd, &pgitRoot, &remote, &premote); err != nil {
			return ToolStats{}, fmt.Errorf("store: tool stats: %w", err)
		}
		if tool == "" {
			tool = UnknownTool
		}
		var key string
		switch f.By {
		case ToolByDay:
			t := parseTime(tsv)
			if t.IsZero() {
				key = "unknown"
			} else {
				key = t.In(loc).Format("2006-01-02")
			}
		case ToolBySession:
			key = session
		case ToolByRepo:
			key = repo
		case ToolByProject:
			var dir bool
			key, dir = activityKey(DimProject, "", parentID, cwd, gitRoot, pcwd, pgitRoot, "", "")
			if f.Project != nil {
				key, dir = mapProject(f.Project, key, dir, remote, premote, pcwd, pgitRoot)
			}
			if seen, ok := isDir[key]; !ok || seen {
				isDir[key] = dir
			}
		}
		out.Total++
		total[tool]++
		if f.By != ToolNoGroup {
			if groups[key] == nil {
				groups[key] = toolTally{}
			}
			groups[key][tool]++
		}
	}
	if err := rows.Err(); err != nil {
		return ToolStats{}, fmt.Errorf("store: tool stats: %w", err)
	}
	out.ByTool = total.list()
	if f.By == ToolNoGroup {
		if f.TopN > 0 && len(out.ByTool) > f.TopN {
			other := 0
			for _, c := range out.ByTool[f.TopN:] {
				other += c.Count
			}
			out.ByTool = append(out.ByTool[:f.TopN:f.TopN], ToolCount{Tool: OtherKey, Count: other})
		}
		return out, nil
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	if f.By == ToolByDay {
		sort.Strings(keys)
	} else {
		sort.Slice(keys, func(i, j int) bool {
			ci, cj := groups[keys[i]].count(), groups[keys[j]].count()
			if ci != cj {
				return ci > cj
			}
			return keys[i] < keys[j]
		})
		if f.TopN > 0 && len(keys) > f.TopN {
			other := toolTally{}
			for _, k := range keys[f.TopN:] {
				for tool, n := range groups[k] {
					other[tool] += n
				}
				delete(groups, k)
			}
			keys = append(keys[:f.TopN:f.TopN], OtherKey)
			groups[OtherKey] = other
		}
	}
	for _, k := range keys {
		out.Groups = append(out.Groups, ToolGroupStats{Key: k, Count: groups[k].count(), ByTool: groups[k].list()})
		if isDir[k] {
			out.Dirs = append(out.Dirs, k)
		}
	}
	sort.Strings(out.Dirs)
	return out, nil
}
