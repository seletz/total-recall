package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/mcp"
	"github.com/seletz/total-recall/internal/ops"
	"github.com/seletz/total-recall/internal/version"
)

// mcpInstructions is what a client shows the model about the server.
const mcpInstructions = `Read-only access to the local archive of past coding-agent sessions. ` +
	`Start with search (by_session=true gives one row per session), then show a session around a hit ` +
	`(from_event, limit). Results are the JSON of the total-recall CLI. Lists are bounded: when the first ` +
	`content item is cut, a second one says so and, where possible, gives next.offset.`

var (
	pSince = mcp.Param{Name: "since", Type: "string", Description: "from this time, local: YYYY-MM-DD, YYYY-MM-DDTHH:MM or RFC 3339"}
	pUntil = mcp.Param{Name: "until", Type: "string", Description: "until this time; a date alone includes the whole day"}
	pRepo  = mcp.Param{Name: "repo", Type: "string", Description: "repo substring (remote URL, git root or cwd)"}
)

func withLimit(def, max int, extra ...mcp.Param) []mcp.Param {
	return append(extra, mcp.Param{Name: "limit", Type: "integer", Max: max, Default: def,
		Description: fmt.Sprintf("maximum number of results (default %d, at most %d); for search per type", def, max)})
}

// mcpTools is the stable tool interface of `total-recall mcp`: each tool is
// one read command with a fixed set of flags. Names and parameters only
// grow within a major version.
func mcpTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "search", Title: "Search the archive", Command: []string{"search"}, FixedFlags: []string{"--json"},
			Description: "Full-text search over the text of past agent sessions. Every word must occur (words are quoted); with raw=true the query is FTS5 syntax (AND, OR, NOT, \"phrase\", prefix*). " +
				"by_session=true (type event only) returns one row per session with first_hit, last_hit, hits and a snippet: the right first query. Same JSON as `total-recall search --json`.",
			Defaults: map[string]int{"limit": 20},
			Params: withLimit(20, 200,
				mcp.Param{Name: "query", Type: "string", Required: true, Positional: true, Description: "words to search for"},
				mcp.Param{Name: "type", Type: "list", Enum: []string{"event", "session", "memory"}, Description: "what to search: event (default; the text of the conversation), session (names and titles), memory (the project memory files); an array or a comma-separated list. Hits come grouped by type in the order given and limit counts per type; each hit has a type field"},
				mcp.Param{Name: "project", Type: "string", Description: "only this project: name as in stats --by project, else a part of name, path or remote URL"},
				mcp.Param{Name: "actor", Type: "string", Enum: []string{"human", "agent", "peer", "automation", "tool"}, Description: "only events of this actor; human = what the user said"},
				mcp.Param{Name: "session", Type: "string", Description: "only this session (full id, native id or unique prefix)"},
				pSince, pUntil, pRepo,
				mcp.Param{Name: "harness", Type: "string", Description: "harness name, e.g. claude"},
				mcp.Param{Name: "by_session", Type: "boolean", Description: "one row per session instead of one row per hit"},
				mcp.Param{Name: "order", Type: "string", Enum: []string{"first_hit", "hits"}, Description: "with by_session: first_hit (oldest first, default) or hits (most hits first)"},
				mcp.Param{Name: "raw", Type: "boolean", Description: "pass query to FTS5 unchanged"},
			),
		},
		{
			Name: "sessions", Title: "List sessions", Command: []string{"sessions"}, FixedFlags: []string{"--json"},
			Description: "List archived sessions, newest first, with id, name, repo, times, duration and event counts. Same JSON as `total-recall sessions --json`.",
			Defaults:    map[string]int{"limit": 50},
			Params: withLimit(50, 500, pSince, pUntil, pRepo,
				mcp.Param{Name: "harness", Type: "string", Description: "harness name, e.g. claude"},
				mcp.Param{Name: "name", Type: "string", Description: "session name substring"},
				mcp.Param{Name: "account", Type: "string", Description: "account name, id or prefix"},
				mcp.Param{Name: "subagents", Type: "boolean", Description: "include subagent sessions"},
			),
		},
		{
			Name: "show", Title: "Show a session", Command: []string{"show"}, FixedFlags: []string{"--json"}, Offset: "offset",
			Description: "Show the conversation of one session (human and agent messages; all=true adds tool calls and results). Select a window with since/until, from_event (an event_id of a search hit) or limit with offset. " +
				"Without limit at most 200 events are returned. Same JSON as `total-recall show --json`.",
			Defaults: map[string]int{"limit": 200},
			Params: withLimit(200, 1000,
				mcp.Param{Name: "session", Type: "string", Required: true, Positional: true, Description: "session id, native id or unique prefix"},
				pSince, pUntil,
				mcp.Param{Name: "from_event", Type: "string", Description: "start at this event id (inclusive)"},
				mcp.Param{Name: "offset", Type: "integer", Description: "skip the first N events of the window (paging with limit)"},
				mcp.Param{Name: "all", Type: "boolean", Description: "include tool calls and results, automation and system events (multiplies the volume)"},
			),
		},
		{
			Name: "stats", Title: "Activity statistics", Command: []string{"stats"}, FixedFlags: []string{"--json"},
			Description: "Activity numbers per day, repo and hour; with workday the first and last activity per day, with worktime the working time per day and category (summary adds issue/PR references and booking texts), with period or by a time series. Same JSON as `total-recall stats --json`.",
			Params: []mcp.Param{
				{Name: "month", Type: "string", Description: "month YYYY-MM (default: current month)"},
				pSince, pUntil,
				{Name: "period", Type: "string", Enum: []string{"day", "week", "month"}, Description: "time series per day, week or month"},
				{Name: "by", Type: "string", Enum: []string{"project", "agent", "directory", "account"}, Description: "break the series down by this key"},
				{Name: "top", Type: "integer", Description: "with by: keep the N largest keys, fold the rest into other"},
				{Name: "workday", Type: "boolean", Description: "first and last activity per day"},
				{Name: "worktime", Type: "boolean", Description: "working time per day and category"},
				{Name: "sessions", Type: "boolean", Description: "with worktime: list the sessions of every day and category"},
				{Name: "summary", Type: "boolean", Description: "with worktime: issue/PR references and booking text"},
				{Name: "range", Type: "string", Description: "with workday or worktime: a named range such as this-week (instead of since/until)"},
				{Name: "all_events", Type: "boolean", Description: "with workday: count human and agent events, not only human messages"},
			},
		},
		{
			Name: "memory", Title: "Project memory", Command: []string{"memory"}, FixedFlags: []string{"--json"},
			Description: "The archived project memory of the harness (Claude Code: the memory files of every project): the files as they are now (or at a date), the versions of one file (history), or a text search over the current content (grep). Same JSON as `total-recall memory --json`.",
			Params: []mcp.Param{
				{Name: "project", Type: "string", Description: "only projects whose name, path or harness key matches"},
				{Name: "at", Type: "string", Description: "the files as they were at this date (local time)"},
				{Name: "history", Type: "string", Description: "the versions of this file (path relative to the memory directory, e.g. MEMORY.md)"},
				{Name: "grep", Type: "string", Description: "full-text search over the current content"},
				{Name: "deleted", Type: "boolean", Description: "include deleted files"},
				{Name: "raw", Type: "boolean", Description: "with grep: FTS5 syntax"},
				{Name: "limit", Type: "integer", Max: 200, Description: "with grep: maximum number of hits"},
			},
		},
		{
			Name: "status", Title: "Archive status", Command: []string{"status"}, FixedFlags: []string{"--json"},
			Description: "State of the archive: location, schema version (and whether a migration is pending), size, counts, last ingest, last backup. Check it when another tool reports an error. Same JSON as `total-recall status --json`.",
		},
	}
}

// runCLI executes the command line argv in-process and returns its stdout.
// The same code path as the shell, so the JSON and the error contract are
// the CLI's; a fresh read-only open per call also picks up an archive that
// maintenance swapped or migrate upgraded.
func (e *env) runCLI(ctx context.Context, argv []string) (string, bool) {
	var out bytes.Buffer
	app := &App{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard,
		Args: e.globalArgs(argv), Getenv: e.app.Getenv, Home: e.app.Home, Now: e.app.Now,
		TerminalWidth: func() int { return 0 },
	}
	app.defaults()
	root, _ := newRoot(app)
	root.SetArgs(app.Args)
	if err := root.ExecuteContext(ctx); err != nil {
		if out.Len() == 0 {
			writeJSONError(&out, err)
		}
		return out.String(), true
	}
	return out.String(), false
}

// globalArgs prepends the persistent flags the server itself was started
// with (--config, --db, --tz, --backup-dir).
func (e *env) globalArgs(argv []string) []string {
	var pre []string
	for _, f := range []struct{ name, v string }{{"config", e.g.config}, {"db", e.g.db}, {"tz", e.g.tz}, {"backup-dir", e.g.backupDir}} {
		if f.v != "" {
			pre = append(pre, "--"+f.name+"="+f.v)
		}
	}
	return append(pre, argv...)
}

func (e *env) mcpCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Run the read-only MCP server on stdio (started by the harness)",
		Long: `Run a Model Context Protocol server on stdin/stdout. The harness starts it
as a child process; there is no port and no daemon. It offers read-only
tools (search, sessions, show, stats, memory, status) that return the JSON
of the CLI commands, with bounded output.

Register it with Claude Code: total-recall mcp print --command`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			s := &mcp.Server{Name: ops.MCPServerName, Version: version.Version, Instructions: mcpInstructions,
				Tools: mcpTools(), Run: e.runCLI}
			return s.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	var h, binary string
	var asCommand bool
	p := &cobra.Command{
		Use:   "print",
		Short: "Print the MCP server entry for a harness",
		Long: `Print the mcpServers block for the harness. For Claude Code, merge it into
the project's .mcp.json, or register the server for the user with the
command printed by --command. Nothing is written.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if h != "claude" {
				return usagef("unknown harness %q (supported: claude)", h)
			}
			if asCommand {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), ops.ClaudeMCPAddCommand(binary))
				return err
			}
			return writeJSON(cmd.OutOrStdout(), ops.ClaudeMCPConfig(binary))
		},
	}
	p.Flags().StringVar(&h, "harness", "claude", "harness: claude")
	p.Flags().StringVar(&binary, "binary", "total-recall", "command used in the entry, e.g. an absolute path")
	p.Flags().BoolVar(&asCommand, "command", false, "print the `claude mcp add` command line instead of JSON")
	c.AddCommand(p)
	return c
}
