package cli

import (
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

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
)

// ---- status ----

// StatusJSON is the --json output of status.
type StatusJSON struct {
	DB                  string            `json:"db"`
	DBSizeBytes         int64             `json:"db_size_bytes"`
	SchemaVersion       int               `json:"schema_version"`
	BinarySchemaVersion int               `json:"binary_schema_version"`
	Sessions            int               `json:"sessions"`
	Events              int               `json:"events"`
	Sources             int               `json:"sources"`
	LastIngest          map[string]string `json:"last_ingest"`
	BackupDir           string            `json:"backup_dir"`
	LastBackup          string            `json:"last_backup,omitempty"`
	LastBackupAt        string            `json:"last_backup_at,omitempty"`
	Log                 string            `json:"log"`
	RecentErrors        []string          `json:"recent_errors"`
}

func (e *env) statusCmd() *cobra.Command {
	var asJSON bool
	var nErrors int
	c := &cobra.Command{
		Use:   "status",
		Short: "Show archive location, size, counts, last ingest, last backup and recent errors",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			out := StatusJSON{DB: cfg.DB, SchemaVersion: v, BinarySchemaVersion: store.LatestSchemaVersion(),
				Sessions: s.Sessions, Events: s.Events, Sources: s.Sources, LastIngest: map[string]string{},
				BackupDir: cfg.Backup.Dir, Log: cfg.LogPath(), RecentErrors: []string{}}
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
			out.LastBackup, out.LastBackupAt = bp, isoTime(bt)
			if errs, err := lastErrors(cfg.LogPath(), nErrors); err == nil && errs != nil {
				out.RecentErrors = errs
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), out)
			}
			now, loc := e.app.Now(), cfg.Location()
			w := cmd.OutOrStdout()
			rows := [][]string{
				{"database", cfg.DB},
				{"size", fmt.Sprintf("%.1f MiB", float64(out.DBSizeBytes)/(1<<20))},
				{"schema", fmt.Sprintf("v%d (binary supports v%d)", v, out.BinarySchemaVersion)},
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
			if err := table(w, nil, rows); err != nil {
				return err
			}
			if len(out.RecentErrors) > 0 {
				_, _ = fmt.Fprintln(w, "\nrecent errors:")
				for _, l := range out.RecentErrors {
					_, _ = fmt.Fprintln(w, "  "+l)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	c.Flags().IntVar(&nErrors, "errors", 5, "number of recent error lines from the log")
	return c
}

// ---- sessions ----

// filterFlags are the session filters shared by sessions and search.
type filterFlags struct {
	since, until, repo, harness, name string
	limit                             int
}

func (f *filterFlags) register(c *cobra.Command, defaultLimit int) {
	fl := c.Flags()
	fl.StringVar(&f.since, "since", "", "from this time (YYYY-MM-DD or YYYY-MM-DDTHH:MM, local time)")
	fl.StringVar(&f.until, "until", "", "until this time; a date includes the whole day")
	fl.StringVar(&f.repo, "repo", "", "repo substring (remote URL, git root or cwd)")
	fl.StringVar(&f.harness, "harness", "", "harness name, e.g. claude")
	fl.StringVar(&f.name, "name", "", "session name substring")
	fl.IntVar(&f.limit, "limit", defaultLimit, "maximum number of results (0 = no limit)")
}

func (f *filterFlags) filter(loc *time.Location) (store.SessionFilter, error) {
	since, err := parseTimeArg(f.since, loc, false)
	if err != nil {
		return store.SessionFilter{}, err
	}
	until, err := parseTimeArg(f.until, loc, true)
	if err != nil {
		return store.SessionFilter{}, err
	}
	return store.SessionFilter{Since: since, Until: until, Repo: f.repo, Harness: f.harness, Name: f.name, Limit: f.limit}, nil
}

// SessionJSON is a session in JSON output.
type SessionJSON struct {
	ID              string `json:"id"`
	Harness         string `json:"harness"`
	NativeID        string `json:"native_id"`
	ParentID        string `json:"parent_id,omitempty"`
	DisplayName     string `json:"display_name"`
	Name            string `json:"name,omitempty"`
	AITitle         string `json:"ai_title,omitempty"`
	AgentName       string `json:"agent_name,omitempty"`
	Cwd             string `json:"cwd"`
	GitRoot         string `json:"git_root,omitempty"`
	GitRemoteURL    string `json:"git_remote_url,omitempty"`
	Repo            string `json:"repo"`
	GitBranch       string `json:"git_branch,omitempty"`
	Model           string `json:"model,omitempty"`
	HarnessVersion  string `json:"harness_version,omitempty"`
	StartedAt       string `json:"started_at"`
	EndedAt         string `json:"ended_at"`
	DurationSeconds int64  `json:"duration_seconds"`
	HumanEvents     int    `json:"human_events"`
	AgentEvents     int    `json:"agent_events"`
	Events          int    `json:"events"`
}

func sessionJSON(s store.SessionInfo) SessionJSON {
	return SessionJSON{ID: s.ID, Harness: s.Harness, NativeID: s.NativeID, ParentID: s.ParentID,
		DisplayName: s.DisplayName(), Name: s.Name, AITitle: s.AITitle, AgentName: s.AgentName,
		Cwd: s.Cwd, GitRoot: s.GitRoot, GitRemoteURL: s.GitRemoteURL, Repo: s.Repo(), GitBranch: s.GitBranch,
		Model: s.Model, HarnessVersion: s.HarnessVersion, StartedAt: isoTime(s.StartedAt), EndedAt: isoTime(s.EndedAt),
		DurationSeconds: int64(s.EndedAt.Sub(s.StartedAt).Seconds()),
		HumanEvents:     s.HumanEvents, AgentEvents: s.AgentEvents, Events: s.Events}
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
	var asJSON, subagents, allSessions bool
	c := &cobra.Command{
		Use:   "sessions",
		Short: "List sessions (newest first)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			sf, err := f.filter(cfg.Location())
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
			if asJSON {
				out := make([]SessionJSON, 0, len(list))
				for _, s := range list {
					out = append(out, sessionJSON(s))
				}
				return writeJSON(cmd.OutOrStdout(), out)
			}
			loc := cfg.Location()
			rows := make([][]string, 0, len(list))
			for _, s := range list {
				rows = append(rows, []string{localTime(s.StartedAt, loc), duration(s.EndedAt.Sub(s.StartedAt)),
					shortID(s.Session), clip(s.DisplayName(), 40), shortRepo(s.Repo()), clip(s.GitBranch, 24),
					strconv.Itoa(s.HumanEvents), strconv.Itoa(s.AgentEvents)})
			}
			return table(cmd.OutOrStdout(), []string{"START", "DUR", "ID", "NAME", "REPO", "BRANCH", "HUMAN", "AGENT"}, rows)
		},
	}
	f.register(c, 0)
	c.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	c.Flags().BoolVar(&subagents, "subagents", false, "include subagent sessions")
	c.Flags().BoolVar(&allSessions, "all-sessions", false, "include sessions without events and start time (e.g. only a title was seen)")
	return c
}

// ---- show ----

// EventJSON is an event in JSON output (without the raw line).
type EventJSON struct {
	ID        string `json:"id"`
	TS        string `json:"ts"`
	Kind      string `json:"kind"`
	Role      string `json:"role"`
	Actor     string `json:"actor"`
	Text      string `json:"text"`
	ToolName  string `json:"tool_name,omitempty"`
	Model     string `json:"model,omitempty"`
	TokensIn  int64  `json:"tokens_in,omitempty"`
	TokensOut int64  `json:"tokens_out,omitempty"`
}

func (e *env) showCmd() *cobra.Command {
	var asJSON, all bool
	c := &cobra.Command{
		Use:   "show SESSION",
		Short: "Show the conversation of one session (full ID, native ID or unique prefix)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			st, err := e.openRead(cfg)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			s, err := st.FindSession(cmd.Context(), args[0])
			switch {
			case errors.Is(err, store.ErrNotFound):
				return fmt.Errorf("no session matches %q", args[0])
			case errors.Is(err, store.ErrAmbiguous):
				return fmt.Errorf("%q matches more than one session; use a longer prefix", args[0])
			case err != nil:
				return err
			}
			evs, err := st.Events(cmd.Context(), s.ID, all)
			if err != nil {
				return err
			}
			if asJSON {
				out := struct {
					Session SessionJSON `json:"session"`
					Events  []EventJSON `json:"events"`
				}{Session: sessionJSON(s), Events: make([]EventJSON, 0, len(evs))}
				for _, ev := range evs {
					out.Events = append(out.Events, EventJSON{ID: ev.ID, TS: isoTime(ev.TS), Kind: string(ev.Kind),
						Role: ev.Role, Actor: string(ev.Actor), Text: ev.Text, ToolName: ev.ToolName, Model: ev.Model,
						TokensIn: ev.TokensIn, TokensOut: ev.TokensOut})
				}
				return writeJSON(cmd.OutOrStdout(), out)
			}
			loc := cfg.Location()
			w := cmd.OutOrStdout()
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
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	c.Flags().BoolVar(&all, "all", false, "include tool calls/results, automation, system and meta events")
	return c
}

// ---- search ----

// HitJSON is a search hit in JSON output.
type HitJSON struct {
	SessionID   string  `json:"session_id"`
	SessionName string  `json:"session_name"`
	Repo        string  `json:"repo"`
	EventID     string  `json:"event_id"`
	TS          string  `json:"ts"`
	Actor       string  `json:"actor"`
	Kind        string  `json:"kind"`
	Snippet     string  `json:"snippet"`
	Rank        float64 `json:"rank"`
}

func (e *env) searchCmd() *cobra.Command {
	var f filterFlags
	var asJSON, raw bool
	var actor string
	c := &cobra.Command{
		Use:   "search QUERY",
		Short: "Full-text search over event text (FTS5) with snippets",
		Long: `Full-text search over event text. By default every word must occur
(words are quoted, so special characters are safe). With --raw the query is
passed to SQLite FTS5 unchanged: AND, OR, NOT, "exact phrase", prefix*.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			sf, err := f.filter(cfg.Location())
			if err != nil {
				return err
			}
			st, err := e.openRead(cfg)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			hits, err := st.Search(cmd.Context(), strings.Join(args, " "), store.SearchFilter{SessionFilter: sf, Actor: actor, Raw: raw})
			if err != nil {
				return err
			}
			if asJSON {
				out := make([]HitJSON, 0, len(hits))
				for _, h := range hits {
					out = append(out, HitJSON{SessionID: h.Event.SessionID, SessionName: h.SessionName, Repo: h.Repo,
						EventID: h.Event.ID, TS: isoTime(h.Event.TS), Actor: string(h.Event.Actor), Kind: string(h.Event.Kind),
						Snippet: h.Snippet, Rank: h.Rank})
				}
				return writeJSON(cmd.OutOrStdout(), out)
			}
			w := cmd.OutOrStdout()
			loc := cfg.Location()
			for _, h := range hits {
				_, _ = fmt.Fprintf(w, "%s  %s  %s  %s  [%s]\n    %s\n", localTime(h.Event.TS, loc), clip(h.SessionName, 40),
					shortRepo(h.Repo), h.Event.SessionID, h.Event.Actor, clip(h.Snippet, 200))
			}
			if len(hits) == 0 {
				_, _ = fmt.Fprintln(e.app.Stderr, "no matches")
			}
			return nil
		},
	}
	f.register(c, 20)
	c.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	c.Flags().BoolVar(&raw, "raw", false, "pass QUERY to FTS5 unchanged (AND/OR/NOT, phrases, prefix*)")
	c.Flags().StringVar(&actor, "actor", "", "only events of this actor: human, agent, peer, automation, tool")
	return c
}

// ---- stats ----

// ActivityJSON is a stats bucket.
type ActivityJSON struct {
	Sessions int `json:"sessions"`
	Human    int `json:"human"`
	Agent    int `json:"agent"`
}

// StatsJSON is the --json output of stats.
type StatsJSON struct {
	Since    string       `json:"since"`
	Until    string       `json:"until"`
	Timezone string       `json:"timezone"`
	Total    ActivityJSON `json:"total"`
	ByDay    []struct {
		Day string `json:"day"`
		ActivityJSON
	} `json:"by_day"`
	ByRepo []struct {
		Repo string `json:"repo"`
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
	out := StatsJSON{Since: isoTime(since), Until: isoTime(until), Timezone: tz, Total: act(s.Total)}
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
		Repo string `json:"repo"`
		ActivityJSON
	}, 0, len(s.ByRepo))
	for _, r := range s.ByRepo {
		out.ByRepo = append(out.ByRepo, struct {
			Repo string `json:"repo"`
			ActivityJSON
		}{r.Repo, act(r.Activity)})
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
	var asJSON, workday, humanOnly bool
	c := &cobra.Command{
		Use:   "stats",
		Short: "Activity per day, per repo and per weekday×hour, per period × project/agent/directory, or first/last activity per day",
		Long: `Counts sessions, human and agent events in a time range, per local day, per
repository and per weekday × hour. Default range: the current month.

With --period or --by the output is a time series instead: one row per
period (day, week or month, local time) and, with --by, per project, agent
type (main or subagent) or working directory. --top N keeps the N largest
keys over the whole range and folds the rest into "other".

With --workday the output is the first and the last activity per day (for
filling in forgotten clock-ins): one row per day with the local clock times
and the elapsed span, plus a total. A day begins at the configured day_start
(default 04:00): activity before it counts for the previous day and is shown
as e.g. "01:30 (+1)". The range comes from --range today|this-week|last-week|
this-month|last-month (relative to now, weeks start on Monday), from
--since/--until or --month; default this-week. --human-only counts only
human messages.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			loc := cfg.Location()
			series := period != "" || by != "" || cmd.Flags().Changed("top")
			if err := checkWorkdayFlags(workday, series, rng, humanOnly, period, by); err != nil {
				return err
			}
			if workday {
				since, until, err := workdayRange(rng, month, sinceArg, untilArg, e.app.Now(), loc, cfg.DayStartOffset())
				if err != nil {
					return err
				}
				st, err := e.openRead(cfg)
				if err != nil {
					return err
				}
				defer func() { _ = st.Close() }()
				days, err := st.Workday(cmd.Context(), store.WorkdayFilter{
					Since: since, Until: until, Location: loc, DayStart: cfg.DayStartOffset(), HumanOnly: humanOnly,
				})
				if err != nil {
					return err
				}
				if asJSON {
					return writeJSON(cmd.OutOrStdout(), workdayJSON(days, since, until, loc, cfg.Timezone, cfg.DayStart, humanOnly))
				}
				return writeWorkdayTable(cmd.OutOrStdout(), days, since, until, loc, cfg.DayStart, humanOnly)
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
				as, err := st.ActivitySeries(cmd.Context(), store.ActivityFilter{
					Since: since, Until: until, Location: loc,
					Period: opts.period, Dimension: opts.by, TopN: opts.top,
				})
				if err != nil {
					return err
				}
				if asJSON {
					return writeJSON(cmd.OutOrStdout(), seriesJSON(as, opts, keyNames(as, opts, ""), since, until, cfg.Timezone))
				}
				return writeSeriesTable(cmd.OutOrStdout(), seriesJSON(as, opts, keyNames(as, opts, cfg.Home), since, until, cfg.Timezone))
			}
			s, err := st.Stats(cmd.Context(), store.StatsFilter{Since: since, Until: until, Location: loc})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), statsJSON(s, since, until, cfg.Timezone))
			}
			return writeStatsTable(cmd.OutOrStdout(), s, since, until, loc)
		},
	}
	c.Flags().StringVar(&month, "month", "", "month YYYY-MM (default: current month)")
	c.Flags().StringVar(&sinceArg, "since", "", "from this time (instead of --month)")
	c.Flags().StringVar(&untilArg, "until", "", "until this time; a date includes the whole day")
	c.Flags().StringVar(&period, "period", "", "time series per day, week or month (instead of the day/repo/hour tables)")
	c.Flags().StringVar(&by, "by", "", "break the series down by project, agent or directory (implies --period day)")
	c.Flags().IntVar(&top, "top", 0, "with --by: keep the N largest keys, fold the rest into \"other\" (0 = all)")
	c.Flags().BoolVar(&workday, "workday", false, "first and last activity per day (default range: this-week)")
	c.Flags().StringVar(&rng, "range", "", "with --workday: "+strings.Join(rangeNames, ", ")+" (instead of --since/--until)")
	c.Flags().BoolVar(&humanOnly, "human-only", false, "with --workday: count human messages only")
	c.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return c
}

// checkWorkdayFlags rejects flag combinations across the three modes of
// stats: --range and --human-only belong to --workday, which excludes the
// series flags.
func checkWorkdayFlags(workday, series bool, rng string, humanOnly bool, period, by string) error {
	switch {
	case !workday && rng != "":
		return fmt.Errorf("--range requires --workday")
	case !workday && humanOnly:
		return fmt.Errorf("--human-only requires --workday")
	case workday && period != "":
		return fmt.Errorf("--workday cannot be combined with --period")
	case workday && by != "":
		return fmt.Errorf("--workday cannot be combined with --by")
	case workday && series:
		return fmt.Errorf("--workday cannot be combined with --top")
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
		return time.Time{}, time.Time{}, fmt.Errorf("use either --range or --month/--since/--until")
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
		return time.Time{}, time.Time{}, fmt.Errorf("use either --month or --since/--until")
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
			return t, t, fmt.Errorf("invalid --month %q (use YYYY-MM)", month)
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
	var format string
	var asJSON bool
	c := &cobra.Command{
		Use:   "sql QUERY",
		Short: "Run a read-only SQL query (table, JSON or CSV output)",
		Long: `Run SQL against the archive. The database is opened with mode=ro and
PRAGMA query_only, so writes are impossible. Schema: total-recall llm --skill.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON {
				format = "json"
			}
			if format != "table" && format != "json" && format != "csv" {
				return fmt.Errorf("invalid --format %q (table, json, csv)", format)
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
			cols, rows, err := st.Query(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeRows(cmd.OutOrStdout(), format, cols, rows)
		},
	}
	c.Flags().StringVar(&format, "format", "table", "output format: table, json, csv")
	c.Flags().BoolVar(&asJSON, "json", false, "shortcut for --format json")
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

// writeRows prints query results; JSON objects keep the column order.
func writeRows(w io.Writer, format string, cols []string, rows [][]any) error {
	switch format {
	case "csv":
		cw := csv.NewWriter(w)
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
	case "json":
		out := make([]orderedRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, orderedRow{cols: cols, vals: r})
		}
		return writeJSON(w, out)
	}
	srows := make([][]string, 0, len(rows))
	for _, r := range rows {
		rec := make([]string, len(r))
		for i, v := range r {
			rec[i] = clip(cell(v), 80)
		}
		srows = append(srows, rec)
	}
	return table(w, cols, srows)
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
