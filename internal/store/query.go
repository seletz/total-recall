package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/model"
)

// SessionFilter selects sessions. Zero fields do not filter.
type SessionFilter struct {
	// Since/Until select sessions active in [Since, Until): ended_at >= Since
	// and started_at < Until. For Search they apply to the event time instead.
	Since, Until time.Time
	// Repo is a case-insensitive substring of git remote URL, git root or cwd.
	Repo string
	// Harness is an exact harness name.
	Harness string
	// Name is a case-insensitive substring of name, AI title or agent name.
	Name string
	// Account is the account ID of the session or a prefix of it.
	Account string
	// TopLevel excludes subagent sessions (parent_id set).
	TopLevel bool
	// HideEmpty excludes sessions without events and without a start time
	// (e.g. only a title record was seen).
	HideEmpty bool
	// Limit caps the number of results; 0 = no limit.
	Limit int
}

// SessionInfo is a session with event counts and the reverse continuation
// link.
type SessionInfo struct {
	model.Session
	// ContinuedFrom is the session that was continued in this one (the
	// one whose ContinuedIn names this session); empty if none is known.
	// If several claim it, the one that started last.
	ContinuedFrom string
	HumanEvents   int
	AgentEvents   int
	Events        int
}

// Repo returns the session's repository identity: remote URL, else git root, else cwd.
func (s SessionInfo) Repo() string { return repoOf(s.Session) }

func repoOf(s model.Session) string {
	switch {
	case s.GitRemoteURL != "":
		return s.GitRemoteURL
	case s.GitRoot != "":
		return s.GitRoot
	}
	return s.Cwd
}

const repoExpr = `COALESCE(NULLIF(s.git_remote_url, ''), NULLIF(s.git_root, ''), s.cwd, '')`

func likeArg(sub string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(sub) + "%"
}

// sessionWhere builds conditions on alias "s" (and on "e.ts" for time if eventTime).
func (f SessionFilter) where(eventTime bool) ([]string, []any) {
	var cond []string
	var args []any
	if !f.Since.IsZero() {
		if eventTime {
			cond = append(cond, "e.ts >= ?")
		} else {
			cond = append(cond, "COALESCE(s.ended_at, s.started_at) >= ?")
		}
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		if eventTime {
			cond = append(cond, "e.ts < ?")
		} else {
			cond = append(cond, "s.started_at < ?")
		}
		args = append(args, formatTime(f.Until))
	}
	if f.Repo != "" {
		cond = append(cond, `(s.git_remote_url LIKE ? ESCAPE '\' OR s.git_root LIKE ? ESCAPE '\' OR s.cwd LIKE ? ESCAPE '\')`)
		a := likeArg(f.Repo)
		args = append(args, a, a, a)
	}
	if f.Harness != "" {
		cond = append(cond, "s.harness = ?")
		args = append(args, f.Harness)
	}
	if f.TopLevel {
		cond = append(cond, "(s.parent_id IS NULL OR s.parent_id = '')")
	}
	if f.HideEmpty {
		cond = append(cond, "(COALESCE(s.started_at, '') <> '' OR EXISTS (SELECT 1 FROM events x WHERE x.session_id = s.id))")
	}
	if f.Name != "" {
		cond = append(cond, `(s.name LIKE ? ESCAPE '\' OR s.ai_title LIKE ? ESCAPE '\' OR s.agent_name LIKE ? ESCAPE '\')`)
		a := likeArg(f.Name)
		args = append(args, a, a, a)
	}
	if f.Account != "" {
		cond = append(cond, `s.account_id LIKE ? ESCAPE '\'`)
		args = append(args, strings.TrimSuffix(strings.TrimPrefix(likeArg(f.Account), "%"), "%")+"%")
	}
	return cond, args
}

func prefixed(cols, alias string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

const countCols = `(SELECT count(*) FROM events c WHERE c.session_id = s.id AND c.actor = 'human'),
	(SELECT count(*) FROM events c WHERE c.session_id = s.id AND c.actor = 'agent'),
	(SELECT count(*) FROM events c WHERE c.session_id = s.id),
	(SELECT f.id FROM sessions f WHERE f.continued_in = s.id ORDER BY f.started_at DESC, f.id LIMIT 1)`

func (s *Store) querySessions(ctx context.Context, cond []string, args []any, limit int) ([]SessionInfo, error) {
	q := "SELECT " + prefixed(sessionCols, "s") + ", " + countCols + " FROM sessions s"
	if len(cond) > 0 {
		q += " WHERE " + strings.Join(cond, " AND ")
	}
	q += " ORDER BY s.started_at DESC, s.id"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []SessionInfo
	for rows.Next() {
		var si SessionInfo
		var from sql.NullString
		m, err := scanSession(rows, &si.HumanEvents, &si.AgentEvents, &si.Events, &from)
		if err != nil {
			return nil, fmt.Errorf("store: sessions: %w", err)
		}
		si.Session, si.ContinuedFrom = m, from.String
		out = append(out, si)
	}
	return out, rows.Err()
}

// Sessions lists sessions matching f, newest start first.
func (s *Store) Sessions(ctx context.Context, f SessionFilter) ([]SessionInfo, error) {
	cond, args := f.where(false)
	return s.querySessions(ctx, cond, args, f.Limit)
}

// Session returns the session with exactly this ID, or ErrNotFound.
func (s *Store) Session(ctx context.Context, id string) (SessionInfo, error) {
	list, err := s.querySessions(ctx, []string{"s.id = ?"}, []any{id}, 1)
	if err != nil {
		return SessionInfo{}, err
	}
	if len(list) == 0 {
		return SessionInfo{}, ErrNotFound
	}
	return list[0], nil
}

// FindSession resolves a user reference: full ID, native ID, or a unique
// prefix of either. It returns ErrNotFound or ErrAmbiguous.
func (s *Store) FindSession(ctx context.Context, ref string) (SessionInfo, error) {
	if ref == "" {
		return SessionInfo{}, ErrNotFound
	}
	list, err := s.querySessions(ctx, []string{"(s.id = ? OR s.native_id = ?)"}, []any{ref, ref}, 2)
	if err != nil {
		return SessionInfo{}, err
	}
	if len(list) == 0 {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(ref) + "%"
		list, err = s.querySessions(ctx, []string{`(s.id LIKE ? ESCAPE '\' OR s.native_id LIKE ? ESCAPE '\')`}, []any{esc, esc}, 2)
		if err != nil {
			return SessionInfo{}, err
		}
	}
	switch len(list) {
	case 0:
		return SessionInfo{}, ErrNotFound
	case 1:
		return list[0], nil
	}
	return SessionInfo{}, ErrAmbiguous
}

const eventCols = `id, session_id, ts, kind, role, actor, text, tool_name, model, tokens_in, tokens_out, source_path, source_offset, raw`

func scanEvent(sc scanner, extra ...any) (model.Event, error) {
	var e model.Event
	var ts, kind, role, actor, text, tool, mdl, sp, raw sql.NullString
	var tin, tout, so sql.NullInt64
	dest := append([]any{&e.ID, &e.SessionID, &ts, &kind, &role, &actor, &text, &tool, &mdl, &tin, &tout, &sp, &so, &raw}, extra...)
	if err := sc.Scan(dest...); err != nil {
		return e, err
	}
	e.TS = parseTime(ts)
	e.Kind, e.Role, e.Actor, e.Text = model.Kind(kind.String), role.String, model.Actor(actor.String), text.String
	e.ToolName, e.Model, e.TokensIn, e.TokensOut = tool.String, mdl.String, tin.Int64, tout.Int64
	e.SourcePath, e.SourceOffset, e.Raw = sp.String, so.Int64, raw.String
	return e, nil
}

// EventFilter narrows the events of a session (EventWindow). Zero fields do
// not filter.
type EventFilter struct {
	// All includes tool calls/results, automation, system and meta events.
	// Otherwise only the conversation is returned: messages (kind=message)
	// from human, agent and peer actors.
	All bool
	// Since/Until select events with Since <= ts < Until.
	Since, Until time.Time
	// FromEvent starts at this event (inclusive, exact ID) in session order.
	// The event must belong to the session, else ErrNotFound. It need not
	// be part of the conversation: without All, a tool event as FromEvent
	// starts at the next message.
	FromEvent string
	// Offset skips the first Offset events after the other filters.
	Offset int
	// Limit caps the number of events; 0 = no limit.
	Limit int
}

// Events returns the events of a session in order. Without all, only the
// conversation is returned: messages (kind=message) from human, agent and
// peer actors; no tool calls/results, automation, system or meta entries.
func (s *Store) Events(ctx context.Context, sessionID string, all bool) ([]model.Event, error) {
	return s.EventWindow(ctx, sessionID, EventFilter{All: all})
}

// EventWindow returns the events of a session in order (ts, then ingest
// order) that match f.
func (s *Store) EventWindow(ctx context.Context, sessionID string, f EventFilter) ([]model.Event, error) {
	q := "SELECT " + eventCols + " FROM events WHERE session_id = ?"
	args := []any{sessionID}
	if !f.All {
		q += " AND kind = 'message' AND actor IN ('human', 'agent', 'peer')"
	}
	if !f.Since.IsZero() {
		q += " AND ts >= ?"
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		q += " AND ts < ?"
		args = append(args, formatTime(f.Until))
	}
	if f.FromEvent != "" {
		var n int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM events WHERE id = ? AND session_id = ?", f.FromEvent, sessionID).Scan(&n); err != nil {
			return nil, fmt.Errorf("store: events: %w", err)
		}
		if n == 0 {
			return nil, ErrNotFound
		}
		q += " AND (ts, seq) >= (SELECT ts, seq FROM events WHERE id = ?)"
		args = append(args, f.FromEvent)
	}
	q += " ORDER BY ts, seq"
	if f.Limit > 0 || f.Offset > 0 {
		limit := f.Limit
		if limit <= 0 {
			limit = -1 // SQLite: no limit, but OFFSET needs a LIMIT clause
		}
		q += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, max(f.Offset, 0))
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("store: events: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SearchFilter narrows a full-text search.
type SearchFilter struct {
	// SessionFilter filters by session; Since/Until apply to the event time.
	SessionFilter
	// SessionID restricts the search to one session (exact ID).
	SessionID string
	// Actor is an exact actor (human, agent, peer, automation, tool).
	Actor string
	// Raw passes the query to FTS5 unchanged (AND/OR/NOT, "phrases", prefix*).
	// Otherwise every whitespace-separated term is quoted and all must match.
	Raw bool
	// HighlightStart/End wrap matches in snippets; default "[" and "]".
	HighlightStart, HighlightEnd string
}

// SearchHit is one matching event.
type SearchHit struct {
	Event       model.Event
	SessionName string // name, else AI title, else agent name, else native ID
	Repo        string
	Snippet     string
	Rank        float64 // bm25, lower is better
}

// ftsQuery quotes every term so user input cannot produce FTS syntax errors.
func ftsQuery(q string) string {
	terms := strings.Fields(q)
	for i, t := range terms {
		terms[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	return strings.Join(terms, " ")
}

// Search runs a full-text query over event text, best matches first.
func (s *Store) Search(ctx context.Context, query string, f SearchFilter) ([]SearchHit, error) {
	if !f.Raw {
		query = ftsQuery(query)
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	hs, he := f.HighlightStart, f.HighlightEnd
	if hs == "" && he == "" {
		hs, he = "[", "]"
	}
	cond, args := f.where(true)
	cond = append([]string{"events_fts MATCH ?"}, cond...)
	args = append([]any{hs, he, query}, args...)
	if f.Actor != "" {
		cond = append(cond, "e.actor = ?")
		args = append(args, f.Actor)
	}
	if f.SessionID != "" {
		cond = append(cond, "e.session_id = ?")
		args = append(args, f.SessionID)
	}
	q := "SELECT " + prefixed(eventCols, "e") + `, COALESCE(NULLIF(s.name, ''), NULLIF(s.ai_title, ''), NULLIF(s.agent_name, ''), s.native_id, s.id), ` +
		repoExpr + `, snippet(events_fts, 0, ?, ?, '…', 16), bm25(events_fts)
		FROM events_fts
		JOIN events e ON e.seq = events_fts.rowid
		JOIN sessions s ON s.id = e.session_id
		WHERE ` + strings.Join(cond, " AND ") + `
		ORDER BY bm25(events_fts), e.ts DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		e, err := scanEvent(rows, &h.SessionName, &h.Repo, &h.Snippet, &h.Rank)
		if err != nil {
			return nil, fmt.Errorf("store: search: %w", err)
		}
		h.Event = e
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: search: %w", err)
	}
	return out, nil
}

// StatsFilter selects the period for Stats.
type StatsFilter struct {
	Since, Until time.Time      // event time range [Since, Until); zero = open
	Location     *time.Location // time zone for days and hours; default UTC
}

// Activity counts sessions (distinct, with at least one human or agent
// event in the bucket) and human/agent events.
type Activity struct {
	Sessions int
	Human    int
	Agent    int
}

// DayActivity is the activity of one local day (YYYY-MM-DD).
type DayActivity struct {
	Day string
	Activity
}

// RepoActivity is the activity in one repository (see SessionInfo.Repo).
type RepoActivity struct {
	Repo string
	Activity
}

// Stats are aggregations over human and agent events.
type Stats struct {
	Total  Activity
	ByDay  []DayActivity  // sorted by day
	ByRepo []RepoActivity // sorted by events desc, then repo
	// ByWeekdayHour is indexed [weekday][hour], weekday 0 = Monday (ISO).
	ByWeekdayHour [7][24]Activity
}

type bucket struct {
	sessions     map[string]bool
	human, agent int
}

func (b *bucket) add(session, actor string) {
	if b.sessions == nil {
		b.sessions = map[string]bool{}
	}
	b.sessions[session] = true
	if actor == string(model.ActorHuman) {
		b.human++
	} else {
		b.agent++
	}
}

func (b *bucket) activity() Activity {
	return Activity{Sessions: len(b.sessions), Human: b.human, Agent: b.agent}
}

// Stats aggregates human and agent events per local day, weekday×hour and
// repo. Day and hour boundaries are computed in Go in f.Location, since
// SQLite has no time zone database.
func (s *Store) Stats(ctx context.Context, f StatsFilter) (Stats, error) {
	loc := f.Location
	if loc == nil {
		loc = time.UTC
	}
	cond := []string{"e.actor IN ('human', 'agent')"}
	var args []any
	if !f.Since.IsZero() {
		cond = append(cond, "e.ts >= ?")
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		cond = append(cond, "e.ts < ?")
		args = append(args, formatTime(f.Until))
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.ts, e.actor, e.session_id, `+repoExpr+`
		FROM events e JOIN sessions s ON s.id = e.session_id
		WHERE `+strings.Join(cond, " AND "), args...)
	if err != nil {
		return Stats{}, fmt.Errorf("store: stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var total bucket
	days := map[string]*bucket{}
	repos := map[string]*bucket{}
	var wh [7][24]bucket
	for rows.Next() {
		var tsv sql.NullString
		var actor, session, repo string
		if err := rows.Scan(&tsv, &actor, &session, &repo); err != nil {
			return Stats{}, fmt.Errorf("store: stats: %w", err)
		}
		t := parseTime(tsv)
		if t.IsZero() {
			continue
		}
		lt := t.In(loc)
		day := lt.Format("2006-01-02")
		if days[day] == nil {
			days[day] = &bucket{}
		}
		if repos[repo] == nil {
			repos[repo] = &bucket{}
		}
		total.add(session, actor)
		days[day].add(session, actor)
		repos[repo].add(session, actor)
		wd := (int(lt.Weekday()) + 6) % 7
		wh[wd][lt.Hour()].add(session, actor)
	}
	if err := rows.Err(); err != nil {
		return Stats{}, fmt.Errorf("store: stats: %w", err)
	}
	st := Stats{Total: total.activity()}
	for d, b := range days {
		st.ByDay = append(st.ByDay, DayActivity{Day: d, Activity: b.activity()})
	}
	sort.Slice(st.ByDay, func(i, j int) bool { return st.ByDay[i].Day < st.ByDay[j].Day })
	for r, b := range repos {
		st.ByRepo = append(st.ByRepo, RepoActivity{Repo: r, Activity: b.activity()})
	}
	sort.Slice(st.ByRepo, func(i, j int) bool {
		a, b := st.ByRepo[i], st.ByRepo[j]
		if na, nb := a.Human+a.Agent, b.Human+b.Agent; na != nb {
			return na > nb
		}
		return a.Repo < b.Repo
	})
	for d := range wh {
		for h := range wh[d] {
			st.ByWeekdayHour[d][h] = wh[d][h].activity()
		}
	}
	return st, nil
}

// Status holds counts for `total-recall status`.
type Status struct {
	Sessions   int
	Events     int
	Sources    int
	LastIngest map[string]time.Time // per harness, from sources.ingested_at
}

// Status returns row counts and the last ingest time per harness.
func (s *Store) Status(ctx context.Context) (Status, error) {
	st := Status{LastIngest: map[string]time.Time{}}
	if err := s.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM sessions), (SELECT count(*) FROM events), (SELECT count(*) FROM sources)`).
		Scan(&st.Sessions, &st.Events, &st.Sources); err != nil {
		return st, fmt.Errorf("store: status: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT harness, MAX(ingested_at) FROM sources GROUP BY harness`)
	if err != nil {
		return st, fmt.Errorf("store: status: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var h string
		var at sql.NullString
		if err := rows.Scan(&h, &at); err != nil {
			return st, fmt.Errorf("store: status: %w", err)
		}
		st.LastIngest[h] = parseTime(at)
	}
	return st, rows.Err()
}
