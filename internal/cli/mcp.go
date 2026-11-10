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
	`(from_event, limit). Results are the JSON of the total-recall CLI. Every result is bounded (32 KiB). A larger result ` +
	`comes as pages: the first content item is one complete page (valid JSON with the same fields; totals are on every page), ` +
	`a second one says which page it is and gives next.cursor. Pass it unchanged as the cursor argument of the same call to get the next page; ` +
	`all pages together are the full result. Nothing is cut silently.`

var (
	pSince = mcp.Param{Name: "since", Type: "string", Description: "from this time, local: YYYY-MM-DD, YYYY-MM-DDTHH:MM or RFC 3339"}
	pUntil = mcp.Param{Name: "until", Type: "string", Description: "until this time; a date alone includes the whole day"}
	pRepo  = mcp.Param{Name: "repo", Type: "string", Description: "repo substring (remote URL, git root or cwd)"}
)

// pagingNote ends the description of every tool with pages: where the
// paging information is.
const pagingNote = " A result over 32 KiB comes in pages: the first content block is one page of the CLI JSON (totals complete on every page); " +
	"page, next and hint are in the SECOND content block, not in the JSON of the first. Pass next.cursor as cursor, with the same other arguments, to get the next page."

// pCursor is the paging argument of every tool whose result can be split.
var pCursor = mcp.Param{Name: "cursor", Type: "string", Server: true,
	Description: "next.cursor from the second content block of the previous page of the same call (same other arguments); omit for the first page. Large results come in pages"}

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
				"by_session=true (type event only) returns one row per session with first_hit, last_hit, hits and a snippet: the right first query. Same JSON as `total-recall search --json`." + pagingNote,
			Defaults: map[string]int{"limit": 20}, Units: []string{""},
			Params: withLimit(20, 200, pCursor,
				mcp.Param{Name: "query", Type: "string", Positional: true, Description: "words to search for; required unless has_attachment is set (then leaving it out lists the newest events with attachments)"},
				mcp.Param{Name: "type", Type: "list", Enum: []string{"event", "session", "memory"}, Description: "what to search: event (default; the text of the conversation), session (names and titles), memory (the project memory files); an array or a comma-separated list. Hits come grouped by type in the order given and limit counts per type; each hit has a type field"},
				mcp.Param{Name: "project", Type: "string", Description: "only this project: name as in stats --by project, else a part of name, path or remote URL"},
				mcp.Param{Name: "actor", Type: "string", Enum: []string{"human", "agent", "peer", "automation", "tool"}, Description: "only events of this actor; human = what the user said"},
				mcp.Param{Name: "session", Type: "string", Description: "only this session (full id, native id or unique prefix)"},
				pSince, pUntil, pRepo,
				mcp.Param{Name: "harness", Type: "string", Description: "harness name, e.g. claude"},
				mcp.Param{Name: "by_session", Type: "boolean", Description: "one row per session instead of one row per hit"},
				mcp.Param{Name: "order", Type: "string", Enum: []string{"first_hit", "hits"}, Description: "with by_session: first_hit (oldest first, default) or hits (most hits first)"},
				mcp.Param{Name: "raw", Type: "boolean", Description: "pass query to FTS5 unchanged"},
				pHasAttachment("events and sessions", "each event hit then lists its attachments (sha256, media_type, bytes, position) and attachment_count; without query the result is the newest events with attachments, the way to find pasted images and documents"),
			),
		},
		{
			Name: "sessions", Title: "List sessions", Command: []string{"sessions"}, FixedFlags: []string{"--json"},
			Description: "List archived sessions, newest first, with id, name, repo, times, duration and event counts. Same JSON as `total-recall sessions --json`." + pagingNote,
			Defaults:    map[string]int{"limit": 50}, Units: []string{""},
			Params: withLimit(50, 500, pCursor, pSince, pUntil, pRepo,
				mcp.Param{Name: "harness", Type: "string", Description: "harness name, e.g. claude"},
				mcp.Param{Name: "name", Type: "string", Description: "session name substring"},
				mcp.Param{Name: "account", Type: "string", Description: "account name, id or prefix"},
				mcp.Param{Name: "subagents", Type: "boolean", Description: "include subagent sessions"},
				mcp.Param{Name: "project", Type: "string", Description: "only this project: name as in stats --by project, else a part of name, path or remote URL"},
				pHasAttachment("sessions", "attachment_count then counts those attachments"),
				mcp.Param{Name: "actor", Type: "string", Enum: []string{"human", "agent", "peer", "automation", "tool"}, Description: "with has_attachment: whose event carries the attachment (human = pasted by the user, tool = screenshots of tool results)"},
			),
		},
		{
			Name: "show", Title: "Show a session", Command: []string{"show"}, FixedFlags: []string{"--json"}, Units: []string{"events"},
			Description: "Show the conversation of one session (human and agent messages; all=true adds tool calls and results). Select a window with since/until, from_event (an event_id of a search hit) or limit with offset. " +
				"Without limit at most 200 events are returned. Human events carry input_kind (typed, pasted, bash, remote; null when unknown). Same JSON as `total-recall show --json`." + pagingNote,
			Defaults: map[string]int{"limit": 200},
			Params: withLimit(200, 1000, pCursor,
				mcp.Param{Name: "session", Type: "string", Required: true, Positional: true, Description: "session id, native id or unique prefix"},
				pSince, pUntil,
				mcp.Param{Name: "from_event", Type: "string", Description: "start at this event id (inclusive)"},
				mcp.Param{Name: "offset", Type: "integer", Description: "skip the first N events of the window (paging with limit)"},
				mcp.Param{Name: "all", Type: "boolean", Description: "include tool calls and results, automation and system events (multiplies the volume)"},
			),
		},
		{
			Name: "stats", Title: "Activity statistics", Command: []string{"stats"}, FixedFlags: []string{"--json"},
			Narrow:      "drop sessions or summary, set a smaller range (month, since/until, range) or use period/by with top",
			Units:       []string{"groups", "days", "buckets", "by_day", "blocks"},
			Description: "Activity numbers per day, repo and hour; with workday the first and last activity per day (blocks adds the blocks of activity with pauses), with day one date chronologically (blocks with their projects and sessions, pauses, target and balance, time per project; the numbers of workday+blocks and worktime for that day), with worktime the working time per day and category (summary adds issue/PR references and booking texts), with period or by a time series, with tools the number of tool calls per tool (by day, session, repo or project; path filters on the arguments). Same JSON as `total-recall stats --json`." + pagingNote,
			Params: []mcp.Param{
				pCursor,
				{Name: "month", Type: "string", Description: "month YYYY-MM (default: current month)"},
				pSince, pUntil,
				{Name: "period", Type: "string", Enum: []string{"day", "week", "month"}, Description: "time series per day, week or month"},
				{Name: "by", Type: "string", Enum: []string{"project", "agent", "directory", "account", "day", "session", "repo"}, Description: "break the series down by this key; with tools: day, session, repo or project"},
				{Name: "top", Type: "integer", Description: "with by: keep the N largest keys, fold the rest into other; with tools and without by: keep the N most used tools in by_tool, the rest is one entry other"},
				{Name: "tools", Type: "boolean", Description: "count tool calls (tool_use events) per tool instead of activity; by groups them, groups always sum to total"},
				{Name: "path", Type: "string", Description: "with tools: only calls whose arguments match this glob (file tools: the whole path, e.g. */src/*; Bash: anywhere in the command; arguments are cut at 1 KiB); by default only calls of tools whose argument is a path or a command are checked, see path_in"},
				{Name: "path_in", Type: "string", Enum: []string{"paths", "all"}, Description: "with path: paths (default) checks only Bash, Read, Edit, MultiEdit, Write, Glob, Grep and LS; all checks every call, also messages to other sessions, prompts and URLs"},
				{Name: "day", Type: "string", Description: "one day chronologically (YYYY-MM-DD, today or yesterday): the blocks of activity with start, end, seconds, events, projects with seconds and sessions, the pauses, span, target and diff, the events by human and agent and the time per project as worktime estimates it; all_events counts agent events too; not with workday, worktime or a range"},
				{Name: "workday", Type: "boolean", Description: "first and last activity per day"},
				{Name: "blocks", Type: "boolean", Description: "with workday: the blocks of activity per day (a gap above [work] block_gap, default 30m, ends a block) with their sum and the pauses, for booking attendance"},
				{Name: "worktime", Type: "boolean", Description: "working time per day and category"},
				{Name: "sessions", Type: "boolean", Description: "with worktime: list the sessions of every day and category"},
				{Name: "summary", Type: "boolean", Description: "with worktime: issue/PR references and booking text"},
				{Name: "range", Type: "string", Description: "with workday or worktime: a named range such as this-week (instead of since/until)"},
				{Name: "all_events", Type: "boolean", Description: "with workday: count human and agent events, not only human messages"},
			},
		},
		{
			Name: "memory", Title: "Project memory", Command: []string{"memory"}, FixedFlags: []string{"--json"},
			Description: "The archived project memory of the harness (Claude Code: the memory files of every project): the files as they are now (or at a date), the versions of one file (history), or a text search over the current content (grep). Same JSON as `total-recall memory --json`." + pagingNote,
			Units:       []string{""},
			Params: []mcp.Param{
				pCursor,
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
		{
			Name: "timeline", Title: "Timeline across sessions", Command: []string{"timeline"}, FixedFlags: []string{"--json"},
			Description: "What happened in a time window across all sessions, oldest first: by default the human's messages, each with ts_local, session_id, project, input_kind (how a human input arrived: typed, pasted, bash, remote; null when unknown) and a shortened text. " +
				"The right tool for \"was the evening after 20:00 still work, and on what?\". Lists exactly the events stats counts as human in the same window. Same JSON as `total-recall timeline --json`." + pagingNote,
			Units:        []string{""},
			Narrow:       `for an overview of a whole day use fields="ts_local,actor,text" with text_width=80 (the fewest pages); otherwise fewer fields, a smaller text_width, a narrower window (since/until) or limit`,
			TextDefaults: map[string]string{"fields": mcpTimelineFields},
			Params: []mcp.Param{
				pCursor, pSince, pUntil,
				{Name: "project", Type: "string", Description: "only this project: name as in stats --by project, else a part of name, path or remote URL"},
				pRepo,
				{Name: "actor", Type: "string", Enum: []string{"human", "agent", "peer", "automation", "tool", "all"}, Description: "whose events: human (default, what the user said), agent, peer, automation, tool or all; with all=true and no actor: all"},
				{Name: "all", Type: "boolean", Description: "include tool calls and results, system and meta events, and every actor unless actor is set (default: messages only)"},
				{Name: "text_width", Type: "integer", Default: 160, Description: "runes of the text per event, 0 = whole text; text_truncated says when it was cut"},
				{Name: "fields", Type: "string", Default: mcpTimelineFields, Description: "comma separated keys of every event; fewer keys make smaller pages and so fewer of them. all = every key (event_id, ts, ts_local, session_id, session_name, project, repo, actor, input_kind, kind, text, text_chars, text_truncated). The default leaves out ts (ts_local has the offset), session_name, repo and text_chars. A name given twice counts once, the order does not matter, an empty value is this default. For an overview of a whole day: fields=\"ts_local,actor,text\" with text_width=80"},
				{Name: "limit", Type: "integer", Description: "keep only the oldest N events; leave out for all (large windows come in pages)"},
			},
		},
	}
}

// pHasAttachment is the has_attachment argument: --has-attachment=KIND.
func pHasAttachment(what, more string) mcp.Param {
	return mcp.Param{Name: "has_attachment", Type: "string", Enum: []string{"any", "image", "document"},
		Description: "only " + what + " with an attachment of this kind (any, image = image/…, document = everything else, e.g. PDF); " + more}
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
of the CLI commands. A result over 32 KiB comes in pages (cursor argument).

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
