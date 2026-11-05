package cli

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/logfile"
	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
)

// ---- status ----

// StatusJSON is the JSON output of status. Every key is always present;
// LastBackup and LastBackupAt are null without a backup.
//
// Errors come from the log file. RecentErrors lists the ERROR lines of the
// window ErrorsWindow (default "24h"; ErrorsSince is its start), oldest
// first, capped by --errors; RecentErrorsTotal is the count in the window,
// OlderErrors the count before it. With --all-errors the window is null and
// OlderErrors 0. Lines that only report a missing transcript never count.
type StatusJSON struct {
	DB                  string            `json:"db"`
	DBSizeBytes         int64             `json:"db_size_bytes"`
	PageSize            int               `json:"page_size"`
	SchemaVersion       int               `json:"schema_version"`
	BinarySchemaVersion int               `json:"binary_schema_version"`
	MigrationPending    bool              `json:"migration_pending"`
	Sessions            int               `json:"sessions"`
	Events              int               `json:"events"`
	Sources             int               `json:"sources"`
	LastIngest          map[string]string `json:"last_ingest"`
	BackupDir           string            `json:"backup_dir"`
	LastBackup          NullString        `json:"last_backup"`
	LastBackupAt        NullString        `json:"last_backup_at"`
	Log                 string            `json:"log"`
	ErrorsWindow        NullString        `json:"errors_window"`
	ErrorsSince         NullString        `json:"errors_since"`
	RecentErrors        []StatusError     `json:"recent_errors"`
	RecentErrorsTotal   int               `json:"recent_errors_total"`
	OlderErrors         int               `json:"older_errors"`
}

// pageSizeCell renders the page size with a hint when a rebuild with the
// recommended size would make the archive smaller.
func pageSizeCell(n int) string {
	if n >= RecommendedPageSize {
		return strconv.Itoa(n)
	}
	return fmt.Sprintf("%d  (total-recall maintenance rebuilds with %d and saves space)", n, RecommendedPageSize)
}

// schemaCell renders the schema versions with a loud hint while a
// migration is pending.
func schemaCell(db, binary int) string {
	s := fmt.Sprintf("v%d (binary supports v%d)", db, binary)
	if db < binary {
		s += "  MIGRATION PENDING: " + migrateHint
	}
	return s
}

// StatusError is one error line in StatusJSON.RecentErrors. At is null
// when the line has no timestamp.
type StatusError struct {
	At      NullString `json:"at"`
	Message string     `json:"message"`
}

// statusErrorWindow is the age up to which status lists log errors. A
// fixed window is predictable and independent of whether hooks ran since;
// "since the last ingest" would hide an error minutes before a successful
// run and would differ per harness.
const statusErrorWindow = 24 * time.Hour

// statusErrorWindowLabel names statusErrorWindow in the output.
const statusErrorWindowLabel = "24h"

func (e *env) statusCmd() *cobra.Command {
	var of outputFlags
	var nErrors int
	var allErrors bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Show archive location, size, counts, last ingest, last backup and recent errors",
		Long: `Show the state of the archive. Errors are the ERROR lines of the log from
the last 24h; older ones appear only as a count. --all-errors lists every
error of the log regardless of age (without a cap unless --errors is set).
Lines about transcripts that did not exist yet never count as errors.`,
		Args: usageArgs(cobra.NoArgs),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		st, err := e.openRead(cfg)
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
		ctx := cmd.Context()
		s, err := st.Status(ctx)
		if err != nil {
			return err
		}
		v, err := st.SchemaVersion(ctx)
		if err != nil {
			return err
		}
		ps, err := st.PageStats(ctx)
		if err != nil {
			return err
		}
		now, loc := e.app.Now(), cfg.Location()
		out := StatusJSON{DB: cfg.DB, PageSize: ps.PageSize, SchemaVersion: v, BinarySchemaVersion: store.LatestSchemaVersion(), MigrationPending: st.MigrationPending(),
			Sessions: s.Sessions, Events: s.Events, Sources: s.Sources, LastIngest: map[string]string{},
			BackupDir: cfg.Backup.Dir, Log: cfg.LogPath(), RecentErrors: []StatusError{}}
		for _, suffix := range []string{"", "-wal"} {
			if fi, err := os.Stat(cfg.DB + suffix); err == nil {
				out.DBSizeBytes += fi.Size()
			}
		}
		for h, t := range s.LastIngest {
			out.LastIngest[h] = isoTime(t)
		}
		bp, bt, err := store.LastBackup(cfg.Backup.Dir)
		if err != nil {
			return err
		}
		out.LastBackup, out.LastBackupAt = NullString(bp), NullString(isoTime(bt))

		var since time.Time
		cap := nErrors
		if allErrors {
			if !cmd.Flags().Changed("errors") {
				cap = 0
			}
		} else {
			since = now.Add(-statusErrorWindow)
			out.ErrorsWindow = statusErrorWindowLabel
			out.ErrorsSince = NullString(isoTime(since))
		}
		all, err := logfile.ReadErrors(cfg.LogPath())
		if err != nil {
			return err
		}
		recent, older := logfile.SplitErrors(all, since)
		out.RecentErrorsTotal, out.OlderErrors = len(recent), older
		if cap > 0 && len(recent) > cap {
			recent = recent[len(recent)-cap:]
		}
		for _, le := range recent {
			out.RecentErrors = append(out.RecentErrors, StatusError{At: NullString(isoTime(le.At)), Message: le.Message})
		}
		if o.format.IsJSON() {
			return o.object(out)
		}
		w := o.w
		rows := [][]string{
			{"database", cfg.DB},
			{"size", fmt.Sprintf("%.1f MiB", float64(out.DBSizeBytes)/(1<<20))},
			{"page size", pageSizeCell(ps.PageSize)},
			{"schema", schemaCell(v, out.BinarySchemaVersion)},
			{"sessions", strconv.Itoa(s.Sessions)},
			{"events", strconv.Itoa(s.Events)},
			{"sources", strconv.Itoa(s.Sources)},
		}
		harnesses := make([]string, 0, len(s.LastIngest))
		for h := range s.LastIngest {
			harnesses = append(harnesses, h)
		}
		sort.Strings(harnesses)
		for _, h := range harnesses {
			t := s.LastIngest[h]
			rows = append(rows, []string{"last ingest (" + h + ")", localTime(t, loc) + "  (" + age(now, t) + ")"})
		}
		if bp == "" {
			rows = append(rows, []string{"last backup", "none in " + cfg.Backup.Dir})
		} else {
			rows = append(rows, []string{"last backup", bp + "  (" + age(now, bt) + ")"})
		}
		rows = append(rows, []string{"log", cfg.LogPath()})
		if err := o.table(nil, rows); err != nil {
			return err
		}
		head := "errors (all)"
		if !allErrors {
			head = fmt.Sprintf("recent errors (last %s, since %s)", out.ErrorsWindow, localTime(since, loc))
		}
		count := "none"
		if out.RecentErrorsTotal > 0 {
			count = strconv.Itoa(out.RecentErrorsTotal)
			if len(recent) < out.RecentErrorsTotal {
				count += fmt.Sprintf(", newest %d shown", len(recent))
			}
		}
		_, _ = fmt.Fprintf(w, "\n%s: %s\n", head, count)
		for _, le := range recent {
			stamp := "(no timestamp)"
			if !le.At.IsZero() {
				stamp = localTime(le.At, loc) + "  (" + age(now, le.At) + ")"
			}
			_, _ = fmt.Fprintf(w, "  %s  %s\n", stamp, le.Message)
		}
		if older > 0 {
			_, _ = fmt.Fprintf(w, "older errors: %d (not shown; status --all-errors lists them, log: %s)\n", older, cfg.LogPath())
		}
		return nil
	})
	of.register(c)
	c.Flags().IntVar(&nErrors, "errors", 5, "number of error lines to list (newest; 0 = all)")
	c.Flags().BoolVar(&allErrors, "all-errors", false, "list errors of any age, not only the last 24h")
	return c
}

// ---- sessions ----

// filterFlags are the session filters shared by sessions and search.
type filterFlags struct {
	since, until, repo, harness, name, account string
	limit                                      int
}

func (f *filterFlags) register(c *cobra.Command, defaultLimit int) {
	fl := c.Flags()
	fl.StringVar(&f.since, "since", "", "from this time (YYYY-MM-DD or YYYY-MM-DDTHH:MM, local time)")
	fl.StringVar(&f.until, "until", "", "until this time; a date includes the whole day")
	fl.StringVar(&f.repo, "repo", "", "repo substring (remote URL, git root or cwd)")
	fl.StringVar(&f.harness, "harness", "", "harness name, e.g. claude")
	fl.StringVar(&f.name, "name", "", "session name substring")
	fl.StringVar(&f.account, "account", "", "account: a name from [accounts], an account ID or a prefix of it")
	fl.IntVar(&f.limit, "limit", defaultLimit, "maximum number of results (0 = no limit)")
}

// filter builds the store filter; cfg resolves an account name.
func (f *filterFlags) filter(cfg *config.Config) (store.SessionFilter, error) {
	loc := cfg.Location()
	since, err := parseTimeArg(f.since, loc, false)
	if err != nil {
		return store.SessionFilter{}, err
	}
	until, err := parseTimeArg(f.until, loc, true)
	if err != nil {
		return store.SessionFilter{}, err
	}
	return store.SessionFilter{Since: since, Until: until, Repo: f.repo, Harness: f.harness, Name: f.name,
		Account: cfg.ResolveAccount(f.account), Limit: f.limit}, nil
}

// SessionJSON is a session in JSON output. Every key is always present;
// a NullString without a value is null.
type SessionJSON struct {
	ID              string     `json:"id"`
	Harness         string     `json:"harness"`
	NativeID        string     `json:"native_id"`
	ParentID        NullString `json:"parent_id"`
	DisplayName     NullString `json:"display_name"`
	Name            NullString `json:"name"`
	AITitle         NullString `json:"ai_title"`
	AgentName       NullString `json:"agent_name"`
	Cwd             NullString `json:"cwd"`
	GitRoot         NullString `json:"git_root"`
	GitRemoteURL    NullString `json:"git_remote_url"`
	Repo            NullString `json:"repo"`
	GitBranch       NullString `json:"git_branch"`
	Model           NullString `json:"model"`
	HarnessVersion  NullString `json:"harness_version"`
	StartedAt       NullString `json:"started_at"`
	EndedAt         NullString `json:"ended_at"`
	DurationSeconds int64      `json:"duration_seconds"`
	HumanEvents     int        `json:"human_events"`
	AgentEvents     int        `json:"agent_events"`
	Events          int        `json:"events"`
	// ContinuedIn is the session this one was continued in, ContinuedFrom
	// the session that was continued in this one (both full IDs; the
	// target of ContinuedIn may not be archived).
	ContinuedIn   NullString `json:"continued_in"`
	ContinuedFrom NullString `json:"continued_from"`
	// AccountID and OrganizationID are the IDs the harness reported;
	// Account is the name [accounts] gives the account ID, null without one.
	AccountID      NullString `json:"account_id"`
	OrganizationID NullString `json:"organization_id"`
	Account        NullString `json:"account"`
}

// sessionJSON converts a session; accountName maps an account ID to its
// configured name ("" without one).
func sessionJSON(s store.SessionInfo, accountName func(string) string) SessionJSON {
	return SessionJSON{ID: s.ID, Harness: s.Harness, NativeID: s.NativeID, ParentID: NullString(s.ParentID),
		DisplayName: NullString(s.DisplayName()), Name: NullString(s.Name), AITitle: NullString(s.AITitle), AgentName: NullString(s.AgentName),
		Cwd: NullString(s.Cwd), GitRoot: NullString(s.GitRoot), GitRemoteURL: NullString(s.GitRemoteURL), Repo: NullString(s.Repo()),
		GitBranch: NullString(s.GitBranch), Model: NullString(s.Model), HarnessVersion: NullString(s.HarnessVersion),
		StartedAt: NullString(isoTime(s.StartedAt)), EndedAt: NullString(isoTime(s.EndedAt)),
		DurationSeconds: int64(s.EndedAt.Sub(s.StartedAt).Seconds()),
		HumanEvents:     s.HumanEvents, AgentEvents: s.AgentEvents, Events: s.Events,
		ContinuedIn: NullString(s.ContinuedIn), ContinuedFrom: NullString(s.ContinuedFrom),
		AccountID: NullString(s.AccountID), OrganizationID: NullString(s.OrganizationID), Account: NullString(accountName(s.AccountID))}
}

// accountLabel formats an account or organization ID for text output: the
// configured name with the ID in parentheses, else the ID.
func accountLabel(id string, accountName func(string) string) string {
	if n := accountName(id); n != "" {
		return n + " (" + id + ")"
	}
	return id
}

// shortID is a reference accepted by show (unique prefix of the native ID).
func shortID(s model.Session) string {
	n := 8
	if strings.HasPrefix(s.NativeID, "agent-") {
		n = 14
	}
	if len(s.NativeID) <= n {
		return s.NativeID
	}
	return s.NativeID[:n]
}

func (e *env) sessionsCmd() *cobra.Command {
	var f filterFlags
	var of outputFlags
	var subagents, allSessions bool
	c := &cobra.Command{
		Use:   "sessions",
		Short: "List sessions (newest first)",
		Args:  usageArgs(cobra.NoArgs),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		sf, err := f.filter(cfg)
		if err != nil {
			return err
		}
		sf.TopLevel = !subagents
		sf.HideEmpty = !allSessions
		st, err := e.openRead(cfg)
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
		list, err := st.Sessions(cmd.Context(), sf)
		if err != nil {
			return err
		}
		if o.format.IsJSON() {
			out := make([]SessionJSON, 0, len(list))
			for _, s := range list {
				out = append(out, sessionJSON(s, cfg.AccountName))
			}
			return writeList(o, out)
		}
		loc := cfg.Location()
		rows := make([][]string, 0, len(list))
		for _, s := range list {
			rows = append(rows, []string{localTime(s.StartedAt, loc), duration(s.EndedAt.Sub(s.StartedAt)),
				shortID(s.Session), s.DisplayName(), shortRepo(s.Repo()), s.GitBranch,
				strconv.Itoa(s.HumanEvents), strconv.Itoa(s.AgentEvents)})
		}
		return o.table([]string{"START", "DUR", "ID", "NAME", "REPO", "BRANCH", "HUMAN", "AGENT"}, rows)
	})
	f.register(c, 0)
	of.register(c)
	c.Flags().BoolVar(&subagents, "subagents", false, "include subagent sessions")
	c.Flags().BoolVar(&allSessions, "all-sessions", false, "include sessions without events and start time (e.g. only a title was seen)")
	return c
}

// ---- show ----

// EventJSON is an event in JSON output (without the raw line). Every key
// is always present; a NullString without a value is null, token counts
// without a value are 0.
type EventJSON struct {
	ID        string     `json:"id"`
	TS        string     `json:"ts"`
	Kind      string     `json:"kind"`
	Role      NullString `json:"role"`
	Actor     string     `json:"actor"`
	Text      NullString `json:"text"`
	ToolName  NullString `json:"tool_name"`
	Model     NullString `json:"model"`
	TokensIn  int64      `json:"tokens_in"`
	TokensOut int64      `json:"tokens_out"`
}

func eventJSON(ev model.Event) EventJSON {
	return EventJSON{ID: ev.ID, TS: isoTime(ev.TS), Kind: string(ev.Kind),
		Role: NullString(ev.Role), Actor: string(ev.Actor), Text: NullString(ev.Text), ToolName: NullString(ev.ToolName),
		Model: NullString(ev.Model), TokensIn: ev.TokensIn, TokensOut: ev.TokensOut}
}

// ShowJSON is the JSON output of show: the session and its events. With
// jsonl only the events are written, one per line.
type ShowJSON struct {
	Session SessionJSON `json:"session"`
	Events  []EventJSON `json:"events"`
}

// showHintEvents is the number of events above which an unbounded show
// prints a hint on stderr on how to bound the output.
const showHintEvents = 2000

// findSession resolves a session reference (full ID, native ID or unique
// prefix) with user-facing errors.
func findSession(ctx context.Context, st *store.Store, ref string) (store.SessionInfo, error) {
	s, err := st.FindSession(ctx, ref)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return s, errKind(ErrKindNotFound, fmt.Errorf("no session matches %q", ref))
	case errors.Is(err, store.ErrAmbiguous):
		return s, errKind(ErrKindAmbiguous, fmt.Errorf("%q matches more than one session; use a longer prefix", ref))
	}
	return s, err
}

func (e *env) showCmd() *cobra.Command {
	var of outputFlags
	var all bool
	var since, until, fromEvent string
	var limit, offset int
	c := &cobra.Command{
		Use:   "show SESSION",
		Short: "Show the conversation of one session (full ID, native ID or unique prefix)",
		Long: `Show the conversation of one session. Without bounds the whole session is
printed; --since/--until (local time, same formats as search), --limit with
--offset, or --from-event EVENT_ID (e.g. from a search hit) select a window.
Events are in session order; --offset and --limit apply after the time
window.`,
		Args: usageArgs(cobra.ExactArgs(1)),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, args []string, o *output) error {
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		loc := cfg.Location()
		f := store.EventFilter{All: all, FromEvent: fromEvent, Limit: limit, Offset: offset}
		if f.Since, err = parseTimeArg(since, loc, false); err != nil {
			return err
		}
		if f.Until, err = parseTimeArg(until, loc, true); err != nil {
			return err
		}
		if limit < 0 {
			return usagef("--limit must not be negative")
		}
		if offset < 0 {
			return usagef("--offset must not be negative")
		}
		st, err := e.openRead(cfg)
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
		s, err := findSession(cmd.Context(), st, args[0])
		if err != nil {
			return err
		}
		evs, err := st.EventWindow(cmd.Context(), s.ID, f)
		if errors.Is(err, store.ErrNotFound) {
			return errKind(ErrKindNotFound, fmt.Errorf("no event %q in session %s", fromEvent, s.ID))
		}
		if err != nil {
			return err
		}
		bounded := since != "" || until != "" || limit > 0 || fromEvent != ""
		if !bounded && len(evs) > showHintEvents {
			_, _ = fmt.Fprintf(e.app.Stderr, "show: %d events; bound the output with --since/--until, --limit (and --offset) or --from-event EVENT_ID\n", len(evs))
		}
		if o.format.IsJSON() {
			out := ShowJSON{Session: sessionJSON(s, cfg.AccountName), Events: make([]EventJSON, 0, len(evs))}
			for _, ev := range evs {
				out.Events = append(out.Events, eventJSON(ev))
			}
			if o.format == FormatJSONL {
				return writeList(o, out.Events)
			}
			return o.object(out)
		}
		w := o.w
		name := s.DisplayName()
		if name == "" {
			name = "(untitled)"
		}
		_, _ = fmt.Fprintf(w, "%s\n%s\n", name, s.ID)
		hdr := [][]string{
			{"time", localTime(s.StartedAt, loc) + " – " + localTime(s.EndedAt, loc) + "  (" + duration(s.EndedAt.Sub(s.StartedAt)) + ")"},
			{"repo", s.Repo()},
			{"branch", s.GitBranch},
			{"cwd", s.Cwd},
		}
		if s.ParentID != "" {
			hdr = append(hdr, []string{"parent", s.ParentID})
		}
		if s.ContinuedFrom != "" {
			hdr = append(hdr, []string{"continued from", s.ContinuedFrom})
		}
		if s.ContinuedIn != "" {
			hdr = append(hdr, []string{"continued in", s.ContinuedIn})
		}
		if s.AccountID != "" {
			hdr = append(hdr, []string{"account", accountLabel(s.AccountID, cfg.AccountName)})
		}
		if s.OrganizationID != "" {
			hdr = append(hdr, []string{"organization", accountLabel(s.OrganizationID, cfg.AccountName)})
		}
		if err := table(w, nil, hdr); err != nil {
			return err
		}
		for _, ev := range evs {
			label := string(ev.Actor)
			if ev.Kind == model.KindToolUse {
				label = "tool_use " + ev.ToolName
			} else if ev.Kind != model.KindMessage {
				label += " (" + string(ev.Kind) + ")"
			}
			_, _ = fmt.Fprintf(w, "\n── %s  %s\n%s\n", ev.TS.In(loc).Format("2006-01-02 15:04:05"), label, strings.TrimRight(ev.Text, "\n"))
		}
		return nil
	})
	of.register(c)
	c.Flags().BoolVar(&all, "all", false, "include tool calls/results, automation, system and meta events")
	c.Flags().StringVar(&since, "since", "", "from this time (YYYY-MM-DD or YYYY-MM-DDTHH:MM, local time)")
	c.Flags().StringVar(&until, "until", "", "until this time; a date includes the whole day")
	c.Flags().IntVar(&limit, "limit", 0, "maximum number of events (0 = all)")
	c.Flags().IntVar(&offset, "offset", 0, "skip the first N events of the window (paging with --limit)")
	c.Flags().StringVar(&fromEvent, "from-event", "", "start at this event ID (inclusive), e.g. event_id of a search hit")
	return c
}

// ---- search ----

// HitJSON is a search hit in JSON output. Every key is always present; a
// NullString without a value is null.
type HitJSON struct {
	SessionID   string     `json:"session_id"`
	SessionName NullString `json:"session_name"`
	Repo        NullString `json:"repo"`
	EventID     string     `json:"event_id"`
	TS          string     `json:"ts"`
	Actor       string     `json:"actor"`
	Kind        string     `json:"kind"`
	Snippet     NullString `json:"snippet"`
	Rank        float64    `json:"rank"`
}

// SessionHitsJSON is one session of search --by-session in JSON output.
// Every key is always present; a NullString without a value is null.
type SessionHitsJSON struct {
	SessionID   string     `json:"session_id"`
	DisplayName NullString `json:"display_name"`
	Repo        NullString `json:"repo"`
	FirstHit    string     `json:"first_hit"`
	LastHit     string     `json:"last_hit"`
	Hits        int        `json:"hits"`
	Snippet     NullString `json:"snippet"`
}

func (e *env) searchCmd() *cobra.Command {
	var f filterFlags
	var of outputFlags
	var raw, bySession bool
	var actor, session, order string
	c := &cobra.Command{
		Use:   "search QUERY",
		Short: "Full-text search over event text (FTS5) with snippets",
		Long: `Full-text search over event text. By default every word must occur
(words are quoted, so special characters are safe). With --raw the query is
passed to SQLite FTS5 unchanged: AND, OR, NOT, "exact phrase", prefix*.

With --by-session the hits are grouped: one row per session (subagents
included) with the first and last hit, the hit count and the snippet of the
first hit, oldest session first (--order hits for most hits first).`,
		Args: usageArgs(cobra.MinimumNArgs(1)),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, args []string, o *output) error {
		if order != "" && !bySession {
			return usage(errors.New("--order needs --by-session"))
		}
		searchOrder := store.SearchOrder(order)
		switch searchOrder {
		case "", store.OrderFirstHit, store.OrderHits:
		default:
			return usage(fmt.Errorf("--order must be first_hit or hits, not %q", order))
		}
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		sf, err := f.filter(cfg)
		if err != nil {
			return err
		}
		st, err := e.openRead(cfg)
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
		var sessionID string
		if session != "" {
			s, err := findSession(cmd.Context(), st, session)
			if err != nil {
				return err
			}
			sessionID = s.ID
		}
		query := strings.Join(args, " ")
		sfilter := store.SearchFilter{SessionFilter: sf, SessionID: sessionID, Actor: actor, Raw: raw}
		loc := cfg.Location()
		if bySession {
			rows, err := st.SearchBySession(cmd.Context(), query, sfilter, searchOrder)
			if err != nil {
				return errKind(ErrKindSQL, err)
			}
			return e.writeSessionHits(o, rows, loc)
		}
		hits, err := st.Search(cmd.Context(), query, sfilter)
		if err != nil {
			return errKind(ErrKindSQL, err)
		}
		if o.format.IsJSON() {
			out := make([]HitJSON, 0, len(hits))
			for _, h := range hits {
				out = append(out, HitJSON{SessionID: h.Event.SessionID, SessionName: NullString(h.SessionName), Repo: NullString(h.Repo),
					EventID: h.Event.ID, TS: isoTime(h.Event.TS), Actor: string(h.Event.Actor), Kind: string(h.Event.Kind),
					Snippet: NullString(h.Snippet), Rank: h.Rank})
			}
			return writeList(o, out)
		}
		w := o.w
		for _, h := range hits {
			// The snippet line is indented by four spaces.
			_, _ = fmt.Fprintf(w, "%s  %s  %s  %s  [%s]\n    %s\n", localTime(h.Event.TS, loc), o.cell(h.SessionName, 40),
				shortRepo(h.Repo), h.Event.SessionID, h.Event.Actor, o.cell(h.Snippet, o.width-4))
		}
		if len(hits) == 0 {
			_, _ = fmt.Fprintln(e.app.Stderr, "no matches")
		}
		return nil
	})
	f.register(c, 20)
	of.register(c)
	c.Flags().BoolVar(&raw, "raw", false, "pass QUERY to FTS5 unchanged (AND/OR/NOT, phrases, prefix*)")
	c.Flags().StringVar(&actor, "actor", "", "only events of this actor: human, agent, peer, automation, tool")
	c.Flags().StringVar(&session, "session", "", "only this session (full ID, native ID or unique prefix)")
	c.Flags().BoolVar(&bySession, "by-session", false, "one row per session: first and last hit, hit count, snippet of the first hit")
	c.Flags().StringVar(&order, "order", "", "sort order with --by-session: first_hit (default, oldest first) or hits (most first)")
	return c
}

// writeSessionHits writes the rows of search --by-session: JSON with UTC
// times, or a table with local times and the snippet as the last column.
func (e *env) writeSessionHits(o *output, rows []store.SessionHits, loc *time.Location) error {
	if o.format.IsJSON() {
		out := make([]SessionHitsJSON, 0, len(rows))
		for _, r := range rows {
			out = append(out, SessionHitsJSON{SessionID: r.SessionID, DisplayName: NullString(r.SessionName), Repo: NullString(r.Repo),
				FirstHit: isoTime(r.FirstHit), LastHit: isoTime(r.LastHit), Hits: r.Hits, Snippet: NullString(r.Snippet)})
		}
		return writeList(o, out)
	}
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(e.app.Stderr, "no matches")
		return nil
	}
	cells := make([][]string, 0, len(rows))
	for _, r := range rows {
		cells = append(cells, []string{localTime(r.FirstHit, loc), localTime(r.LastHit, loc), strconv.Itoa(r.Hits),
			r.SessionID, r.SessionName, shortRepo(r.Repo), r.Snippet})
	}
	return o.table([]string{"FIRST", "LAST", "HITS", "ID", "NAME", "REPO", "SNIPPET"}, cells)
}

// ---- stats ----

// ActivityJSON is a stats bucket.
type ActivityJSON struct {
	Sessions int `json:"sessions"`
	Human    int `json:"human"`
	Agent    int `json:"agent"`
}

// StatsJSON is the JSON output of stats. Every key is always present; a
// NullString without a value is null.
type StatsJSON struct {
	Since    NullString   `json:"since"`
	Until    NullString   `json:"until"`
	Timezone string       `json:"timezone"`
	Total    ActivityJSON `json:"total"`
	ByDay    []struct {
		Day string `json:"day"`
		ActivityJSON
	} `json:"by_day"`
	ByRepo []struct {
		Repo NullString `json:"repo"`
		ActivityJSON
	} `json:"by_repo"`
	// ByWeekdayHour has all 7×24 cells; weekday 0 = Monday.
	ByWeekdayHour []struct {
		Weekday int    `json:"weekday"`
		Day     string `json:"day"`
		Hour    int    `json:"hour"`
		ActivityJSON
	} `json:"by_weekday_hour"`
}

var weekdays = [7]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

func act(a store.Activity) ActivityJSON {
	return ActivityJSON{Sessions: a.Sessions, Human: a.Human, Agent: a.Agent}
}

func statsJSON(s store.Stats, since, until time.Time, tz string) StatsJSON {
	out := StatsJSON{Since: NullString(isoTime(since)), Until: NullString(isoTime(until)), Timezone: tz, Total: act(s.Total)}
	out.ByDay = make([]struct {
		Day string `json:"day"`
		ActivityJSON
	}, 0, len(s.ByDay))
	for _, d := range s.ByDay {
		out.ByDay = append(out.ByDay, struct {
			Day string `json:"day"`
			ActivityJSON
		}{d.Day, act(d.Activity)})
	}
	out.ByRepo = make([]struct {
		Repo NullString `json:"repo"`
		ActivityJSON
	}, 0, len(s.ByRepo))
	for _, r := range s.ByRepo {
		out.ByRepo = append(out.ByRepo, struct {
			Repo NullString `json:"repo"`
			ActivityJSON
		}{NullString(r.Repo), act(r.Activity)})
	}
	for d := range 7 {
		for h := range 24 {
			out.ByWeekdayHour = append(out.ByWeekdayHour, struct {
				Weekday int    `json:"weekday"`
				Day     string `json:"day"`
				Hour    int    `json:"hour"`
				ActivityJSON
			}{d, weekdays[d], h, act(s.ByWeekdayHour[d][h])})
		}
	}
	return out
}

func (e *env) statsCmd() *cobra.Command {
	var month, sinceArg, untilArg, period, by, rng string
	var top int
	var of outputFlags
	var workday, humanOnly, allEvents, worktime, sessions, summary bool
	c := &cobra.Command{
		Use:   "stats",
		Short: "Activity per day, per repo and per weekday×hour, per period × project/agent/directory, first/last activity per day, or working time per project",
		Long: `Counts sessions, human and agent events in a time range, per local day, per
repository and per weekday × hour. Default range: the current month.

With --period or --by the output is a time series instead: one row per
period (day, week or month, local time) and, with --by, per project, agent
type (main or subagent), working directory or account (named by [accounts]).
--top N keeps the N largest keys over the whole range and folds the rest
into "other".

With --workday the output is the first and the last activity per day (for
filling in forgotten clock-ins): one row per day with the local clock times
and the elapsed span, plus a total. A day begins at the configured day_start
(default 04:00): activity before it counts for the previous day and is shown
as e.g. "01:30 (+1)". The range comes from --range today|this-week|last-week|
this-month|last-month (relative to now, weeks start on Monday), from
--since/--until or --month; default this-week. Only your own messages are
counted (agent events run on at night and stretch the span); --all-events
counts human and agent events like the activity statistics; [work] count
sets the default. Each weekday that is no public holiday is a workday with the
daily target of the [work] config (default 8h): DIFF is the span minus the
target, the total line balances all spans against target × workdays of the
range (up to today). Holidays come from the configured Bundesland and are
named in the HOLIDAY column.

With --worktime the output is the estimated working time per day and
category (for timesheets): the gap between two of your own messages, across
all sessions, counts for the project of the later one, in full up to the
configured max_gap (default 20m), otherwise min_credit (default 3m); the sum
per category and day is rounded to the grid (default up to 15m). Projects
are mapped to categories by the [worktime] rules; a project without a rule
is its own category. Ranges as with --workday. --sessions lists the sessions
and projects of every cell with their titles as a template for booking
texts; --summary adds the issue and pull-request references (#31, PR #32,
owner/repo#123) from the titles and your own messages, per project, and a
booking text "<project>: <title> (<refs>), …" cut to summary_max_chars
(JSON always has all of it). Meetings and work without an agent leave no
messages: the values are a lower bound.`,
		Args: usageArgs(cobra.NoArgs),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		loc := cfg.Location()
		series := period != "" || by != "" || cmd.Flags().Changed("top")
		if err := checkWorkdayFlags(workday, worktime, sessions || summary, series, rng, humanOnly || allEvents, period, by); err != nil {
			return err
		}
		if worktime {
			return e.runWorktime(cmd, cfg, o, rng, month, sinceArg, untilArg, sessions, summary)
		}
		if workday {
			if cmd.Flags().Changed("human-only") {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "stats: --human-only is deprecated: human messages are counted by default, --all-events counts everything")
			}
			humanOnly = (cfg.Work.HumanOnly() || humanOnly) && !allEvents
			work, err := workHours(cfg)
			if err != nil {
				return err
			}
			since, until, err := workdayRange(rng, month, sinceArg, untilArg, e.app.Now(), loc, cfg.DayStartOffset())
			if err != nil {
				return err
			}
			st, err := e.openRead(cfg)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			cal := holidays(cfg)
			days, err := st.Workday(cmd.Context(), store.WorkdayFilter{
				Since: since, Until: until, Location: loc, DayStart: cfg.DayStartOffset(), HumanOnly: humanOnly, Holidays: cal,
			})
			if err != nil {
				return err
			}
			r := workdayReport{
				days: days, since: since, until: until, loc: loc, tz: cfg.Timezone, dayStart: cfg.DayStart, humanOnly: humanOnly,
				work: work, hours: cfg.Work.Hours, region: cal.Region(), asOf: store.DayOf(e.app.Now(), loc, cfg.DayStartOffset()),
			}
			if o.format.IsJSON() {
				return o.object(r.json())
			}
			return r.table(o.w)
		}
		since, until, err := statsPeriod(month, sinceArg, untilArg, e.app.Now().In(loc), loc)
		if err != nil {
			return err
		}
		var opts seriesOptions
		if series {
			if opts, err = parseSeriesOptions(period, by, top); err != nil {
				return err
			}
		}
		st, err := e.openRead(cfg)
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
		if series {
			var projects project.Resolver
			if opts.byName != "" && opts.by == store.DimProject {
				if projects, err = cfg.ProjectResolver(); err != nil {
					return errKind(ErrKindConfig, err)
				}
			}
			as, err := st.ActivitySeries(cmd.Context(), store.ActivityFilter{
				Since: since, Until: until, Location: loc,
				Period: opts.period, Dimension: opts.by, TopN: opts.top, Project: projects.Key,
			})
			if err != nil {
				return err
			}
			if o.format.IsJSON() {
				return o.object(seriesJSON(as, opts, keyNames(as, opts, "", cfg.Accounts, projects), since, until, cfg.Timezone))
			}
			return writeSeriesTable(o.w, seriesJSON(as, opts, keyNames(as, opts, cfg.Home, cfg.Accounts, projects), since, until, cfg.Timezone))
		}
		s, err := st.Stats(cmd.Context(), store.StatsFilter{Since: since, Until: until, Location: loc})
		if err != nil {
			return err
		}
		if o.format.IsJSON() {
			return o.object(statsJSON(s, since, until, cfg.Timezone))
		}
		return writeStatsTable(o.w, s, since, until, loc)
	})
	c.Flags().StringVar(&month, "month", "", "month YYYY-MM (default: current month)")
	c.Flags().StringVar(&sinceArg, "since", "", "from this time (instead of --month)")
	c.Flags().StringVar(&untilArg, "until", "", "until this time; a date includes the whole day")
	c.Flags().StringVar(&period, "period", "", "time series per day, week or month (instead of the day/repo/hour tables)")
	c.Flags().StringVar(&by, "by", "", "break the series down by project, agent or directory (implies --period day)")
	c.Flags().IntVar(&top, "top", 0, "with --by: keep the N largest keys, fold the rest into \"other\" (0 = all)")
	c.Flags().BoolVar(&workday, "workday", false, "first and last activity per day (default range: this-week)")
	c.Flags().BoolVar(&worktime, "worktime", false, "working time per day and category from the gaps between your messages (default range: this-week)")
	c.Flags().BoolVar(&sessions, "sessions", false, "with --worktime: list the sessions and projects of every day and category")
	c.Flags().BoolVar(&summary, "summary", false, "with --worktime: issue/PR references and a booking text per day and category (JSON always has them)")
	c.Flags().StringVar(&rng, "range", "", "with --workday or --worktime: "+strings.Join(rangeNames, ", ")+" (instead of --since/--until)")
	c.Flags().BoolVar(&allEvents, "all-events", false, "with --workday: count human and agent events (default: human messages only)")
	c.Flags().BoolVar(&humanOnly, "human-only", false, "with --workday: count human messages only (the default; deprecated)")
	_ = c.Flags().MarkHidden("human-only")
	of.register(c)
	return c
}

// checkWorkdayFlags rejects flag combinations across the four modes of
// stats: --range belongs to --workday and --worktime, the counting flags to
// --workday and --sessions/--summary (details) to --worktime; both exclude
// the series flags and each other.
func checkWorkdayFlags(workday, worktime, details, series bool, rng string, humanOnly bool, period, by string) error {
	mode := "--workday"
	if worktime {
		mode = "--worktime"
	}
	switch {
	case workday && worktime:
		return usagef("--worktime cannot be combined with --workday")
	case !workday && !worktime && rng != "":
		return usagef("--range requires --workday or --worktime")
	case !workday && humanOnly:
		return usagef("--all-events requires --workday")
	case !worktime && details:
		return usagef("--sessions and --summary require --worktime")
	case (workday || worktime) && period != "":
		return usagef("%s cannot be combined with --period", mode)
	case (workday || worktime) && by != "":
		return usagef("%s cannot be combined with --by", mode)
	case (workday || worktime) && series:
		return usagef("%s cannot be combined with --top", mode)
	}
	return nil
}

// workdayRange resolves the range of --workday: --range, else --month or
// --since/--until, else this week. Bounds are day boundaries (see
// rangeBounds and workdayBounds).
func workdayRange(rng, month, sinceArg, untilArg string, now time.Time, loc *time.Location, dayStart time.Duration) (time.Time, time.Time, error) {
	explicit := month != "" || sinceArg != "" || untilArg != ""
	switch {
	case rng != "" && explicit:
		return time.Time{}, time.Time{}, usagef("use either --range or --month/--since/--until")
	case rng != "":
		return rangeBounds(rng, now, loc, dayStart)
	case explicit:
		return workdayBounds(month, sinceArg, untilArg, loc, dayStart)
	}
	return rangeBounds("this-week", now, loc, dayStart)
}

// statsPeriod resolves --month or --since/--until; default the month of now.
func statsPeriod(month, sinceArg, untilArg string, now time.Time, loc *time.Location) (time.Time, time.Time, error) {
	if month != "" && (sinceArg != "" || untilArg != "") {
		return time.Time{}, time.Time{}, usagef("use either --month or --since/--until")
	}
	if sinceArg != "" || untilArg != "" {
		since, err := parseTimeArg(sinceArg, loc, false)
		if err != nil {
			return since, since, err
		}
		until, err := parseTimeArg(untilArg, loc, true)
		return since, until, err
	}
	var start time.Time
	if month == "" {
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	} else {
		t, err := time.ParseInLocation("2006-01", month, loc)
		if err != nil {
			return t, t, usagef("invalid --month %q (use YYYY-MM)", month)
		}
		start = t
	}
	return start, start.AddDate(0, 1, 0), nil
}

func writeStatsTable(w io.Writer, s store.Stats, since, until time.Time, loc *time.Location) error {
	period := "all time"
	if !since.IsZero() || !until.IsZero() {
		end := ""
		if !until.IsZero() {
			end = localTime(until.Add(-time.Minute), loc)
		}
		period = localTime(since, loc) + " – " + end
	}
	_, _ = fmt.Fprintf(w, "%s (%s)\ntotal: %d sessions, %d human, %d agent events\n\n", period, loc, s.Total.Sessions, s.Total.Human, s.Total.Agent)
	rows := make([][]string, 0, len(s.ByDay))
	for _, d := range s.ByDay {
		rows = append(rows, []string{d.Day, strconv.Itoa(d.Sessions), strconv.Itoa(d.Human), strconv.Itoa(d.Agent)})
	}
	if err := table(w, []string{"DAY", "SESSIONS", "HUMAN", "AGENT"}, rows); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w)
	rows = rows[:0]
	for _, r := range s.ByRepo {
		rows = append(rows, []string{shortRepo(r.Repo), strconv.Itoa(r.Sessions), strconv.Itoa(r.Human), strconv.Itoa(r.Agent)})
	}
	if err := table(w, []string{"REPO", "SESSIONS", "HUMAN", "AGENT"}, rows); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w, "\nhuman events per weekday × hour")
	header := []string{""}
	for h := range 24 {
		header = append(header, fmt.Sprintf("%02d", h))
	}
	rows = rows[:0]
	for d := range 7 {
		r := []string{weekdays[d]}
		for h := range 24 {
			n := s.ByWeekdayHour[d][h].Human
			if n == 0 {
				r = append(r, ".")
			} else {
				r = append(r, strconv.Itoa(n))
			}
		}
		rows = append(rows, r)
	}
	return table(w, header, rows)
}

// ---- sql ----

func (e *env) sqlCmd() *cobra.Command {
	var of outputFlags
	c := &cobra.Command{
		Use:   "sql QUERY",
		Short: "Run a read-only SQL query (table, JSON, JSONL or CSV output)",
		Long: `Run SQL against the archive. The database is opened with mode=ro and
PRAGMA query_only, so writes are impossible. Schema: total-recall llm --skill.`,
		Args: usageArgs(cobra.ExactArgs(1)),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, args []string, o *output) error {
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		st, err := e.openRead(cfg)
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
		cols, rows, err := st.Query(cmd.Context(), args[0])
		if err != nil {
			return errKind(ErrKindSQL, err)
		}
		return writeRows(o, cols, rows)
	})
	of.register(c, FormatCSV)
	return c
}

func cell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// writeRows prints query results; JSON objects keep the column order and
// SQL NULL is null.
func writeRows(o *output, cols []string, rows [][]any) error {
	switch o.format {
	case FormatCSV:
		cw := csv.NewWriter(o.w)
		_ = cw.Write(cols)
		for _, r := range rows {
			rec := make([]string, len(r))
			for i, v := range r {
				rec[i] = cell(v)
			}
			_ = cw.Write(rec)
		}
		cw.Flush()
		return cw.Error()
	case FormatJSON, FormatJSONL:
		out := make([]orderedRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, orderedRow{cols: cols, vals: r})
		}
		return writeList(o, out)
	}
	srows := make([][]string, 0, len(rows))
	for _, r := range rows {
		rec := make([]string, len(r))
		for i, v := range r {
			rec[i] = cell(v)
		}
		srows = append(srows, rec)
	}
	return o.table(cols, srows)
}

// orderedRow marshals as a JSON object with keys in column order.
type orderedRow struct {
	cols []string
	vals []any
}

func (r orderedRow) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, c := range r.cols {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := jsonMarshal(c)
		if err != nil {
			return nil, err
		}
		v, err := jsonMarshal(r.vals[i])
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}
