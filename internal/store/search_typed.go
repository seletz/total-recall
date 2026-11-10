package store

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Typed search (#196): one search over the object types of the archive.
// Every type has its own search function and its own index (events_fts
// for events, memory_fts for memory, the name columns of sessions);
// SearchTyped runs them one after the other and returns the hits grouped
// by type. bm25 values of different indexes are not comparable, so there
// is no rank across types. The type names are harness-neutral and a
// stable interface (CLI --type, JSON field type); a harness adapter maps
// what it delivers onto them.

// HitType is the object type of a search hit.
type HitType string

// The object types. Only the searchable ones (see HitTypes) are accepted
// by SearchTyped; the others are reserved names for types that have no
// text index yet.
const (
	// TypeEvent is an event: a message, a tool call or a tool result (its
	// kind).
	TypeEvent HitType = "event"
	// TypeSession is a session, found by its name, title or agent name.
	TypeSession HitType = "session"
	// TypeMemory is a file of the project memory, found by its current
	// content.
	TypeMemory HitType = "memory"
	// TypeAttachment is an attachment of an event (image, document).
	TypeAttachment HitType = "attachment"
	// TypeMeta is a meta record of a session.
	TypeMeta HitType = "meta"
	// TypeFile is a version of an edited file (file history).
	TypeFile HitType = "file"
)

// TypeInfo describes an object type.
type TypeInfo struct {
	Type HitType
	// Searchable is false for a reserved type without a search.
	Searchable bool
}

// The filters of a TypedQuery, named like the CLI flags of search.
const (
	FilterSince   = "since"
	FilterUntil   = "until"
	FilterRepo    = "repo"
	FilterHarness = "harness"
	FilterName    = "name"
	FilterAccount = "account"
	FilterProject = "project"
	FilterSession = "session"
	FilterActor   = "actor"
	FilterRaw     = "raw"
	// FilterAttachment is SessionFilter.Attachment (CLI --has-attachment).
	FilterAttachment = "has-attachment"
)

// typeDef is the search of one type and the filters it applies.
type typeDef struct {
	t       HitType
	search  func(s *Store, ctx context.Context, q TypedQuery) (TypeHits, error)
	filters []string
}

// typeDefs lists every type in display order; a nil search marks a
// reserved type.
var typeDefs = []typeDef{
	{TypeEvent, (*Store).searchEventHits, []string{FilterSince, FilterUntil, FilterRepo, FilterHarness, FilterName, FilterAccount,
		FilterProject, FilterSession, FilterActor, FilterRaw, FilterAttachment}},
	{TypeSession, (*Store).searchSessionHits, []string{FilterSince, FilterUntil, FilterRepo, FilterHarness, FilterName, FilterAccount,
		FilterProject, FilterSession, FilterAttachment}},
	{TypeMemory, (*Store).searchMemoryHits, []string{FilterSince, FilterUntil, FilterRepo, FilterHarness, FilterProject, FilterRaw}},
	{TypeAttachment, nil, nil},
	{TypeMeta, nil, nil},
	{TypeFile, nil, nil},
}

func typeDefOf(t HitType) (typeDef, bool) {
	for _, d := range typeDefs {
		if d.t == t {
			return d, true
		}
	}
	return typeDef{}, false
}

// HitTypes lists all object types, the searchable ones first.
func HitTypes() []TypeInfo {
	out := make([]TypeInfo, len(typeDefs))
	for i, d := range typeDefs {
		out[i] = TypeInfo{Type: d.t, Searchable: d.search != nil}
	}
	return out
}

func typeNames() string {
	var n []string
	for _, d := range typeDefs {
		n = append(n, string(d.t))
	}
	return strings.Join(n, ", ")
}

// ParseHitTypes parses a comma-separated list of types: blanks around
// names and empty entries are ignored, duplicates count once, the order
// stays. An empty list is TypeEvent. An unknown name or a reserved type
// is an error.
func ParseHitTypes(list string) ([]HitType, error) {
	var out []HitType
	seen := map[HitType]bool{}
	for _, name := range strings.Split(list, ",") {
		t := HitType(strings.TrimSpace(name))
		if t == "" || seen[t] {
			continue
		}
		if err := checkType(t); err != nil {
			return nil, err
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) == 0 {
		out = []HitType{TypeEvent}
	}
	return out, nil
}

func checkType(t HitType) error {
	d, ok := typeDefOf(t)
	if !ok {
		return fmt.Errorf("unknown type %q (%s)", t, typeNames())
	}
	if d.search == nil {
		return fmt.Errorf("type %q is not searchable yet", t)
	}
	return nil
}

// Supports reports whether a type applies a filter (Filter* names).
func Supports(t HitType, filter string) bool {
	d, _ := typeDefOf(t)
	for _, f := range d.filters {
		if f == filter {
			return true
		}
	}
	return false
}

// FilterError reports a filter that a selected type does not apply.
type FilterError struct {
	Filter string
	Type   HitType
}

func (e *FilterError) Error() string {
	return fmt.Sprintf("%s does not apply to type %s", e.Filter, e.Type)
}

// ProjectRef is the raw project key of a hit as the project dimension
// counts it: the git root of the session, else its working directory
// (Dir); a subagent counts with its parent. Remote is the remote URL of
// the repository, empty for a directory. A memory project has the key of
// its working directory. The caller names it with project.Resolver.
type ProjectRef struct {
	Key    string
	Dir    bool
	Remote string
}

func (r ProjectRef) clean() ProjectRef {
	if r.Dir {
		r.Remote = ""
	}
	return r
}

// The raw project key of the session aliased "s" with its parent "p"
// (LEFT JOIN), like activityKey: the parent's key when it has one.
const (
	parentHasKey      = `(COALESCE(p.git_root, '') <> '' OR COALESCE(p.cwd, '') <> '')`
	projectKeyExpr    = `(CASE WHEN ` + parentHasKey + ` THEN COALESCE(NULLIF(p.git_root, ''), p.cwd) ELSE COALESCE(NULLIF(s.git_root, ''), s.cwd, '') END)`
	projectDirExpr    = `(CASE WHEN ` + parentHasKey + ` THEN COALESCE(p.git_root, '') = '' ELSE COALESCE(s.git_root, '') = '' END)`
	projectRemoteExpr = `(CASE WHEN ` + parentHasKey + ` THEN COALESCE(p.git_remote_url, '') ELSE COALESCE(s.git_remote_url, '') END)`
	projectCols       = projectKeyExpr + `, ` + projectDirExpr + `, ` + projectRemoteExpr
)

// inCond returns "<expr> IN (?, …)" with the values as arguments.
func inCond[T any](expr string, vals []T) (string, []any) {
	ph := make([]string, len(vals))
	args := make([]any, len(vals))
	for i, v := range vals {
		ph[i], args[i] = "?", v
	}
	return expr + " IN (" + strings.Join(ph, ", ") + ")", args
}

// ProjectScope restricts a typed search to projects: events and sessions
// by raw project key, memory by memory project ID. Empty lists match
// nothing; a nil *ProjectScope does not restrict.
type ProjectScope struct {
	Keys             []string
	MemoryProjectIDs []int64
}

// TypedQuery is a search over one or more object types.
type TypedQuery struct {
	// Text is the query; Raw passes it to FTS5 unchanged (event, memory),
	// otherwise all words must occur.
	Text string
	Raw  bool
	// Types are searched in this order; empty means TypeEvent.
	Types []HitType
	// SessionFilter: Since/Until apply to the event time (event), the
	// time the session was active (session) and the time of the current
	// version (memory). Repo and Harness apply to all three types, Name
	// and Account to events and sessions. Limit caps the hits per type
	// (0 = no limit).
	SessionFilter
	// SessionID restricts events or sessions to one session.
	SessionID string
	// Actor is an exact actor of events.
	Actor string
	// Project restricts every type to projects; nil does not restrict.
	Project *ProjectScope
	// HighlightStart/End wrap matches in snippets; default "[" and "]".
	HighlightStart, HighlightEnd string
}

// filters lists the filters q uses.
func (q TypedQuery) filters() []string {
	var out []string
	add := func(on bool, f string) {
		if on {
			out = append(out, f)
		}
	}
	add(!q.Since.IsZero(), FilterSince)
	add(!q.Until.IsZero(), FilterUntil)
	add(q.Repo != "", FilterRepo)
	add(q.Harness != "", FilterHarness)
	add(q.Name != "", FilterName)
	add(q.Account != "", FilterAccount)
	add(q.Project != nil, FilterProject)
	add(q.SessionID != "", FilterSession)
	add(q.Actor != "", FilterActor)
	add(q.Raw, FilterRaw)
	add(q.Attachment != nil, FilterAttachment)
	return out
}

func (q TypedQuery) highlight() (string, string) {
	if q.HighlightStart == "" && q.HighlightEnd == "" {
		return "[", "]"
	}
	return q.HighlightStart, q.HighlightEnd
}

// Hit is one search hit of any type. Exactly one of Event, Session and
// Memory is set, the one of Type; viewers switch on Type.
type Hit struct {
	Type HitType
	// TS is the event time, the start of the session or the time of the
	// memory file's current version.
	TS      time.Time
	Snippet string
	// Rank is bm25 (lower is better) for event and memory, 0 for session.
	Rank    float64
	Project ProjectRef
	Event   *SearchHit
	Session *SessionInfo
	Memory  *MemoryHit
}

// TypeHits are the hits of one type, best first (sessions: newest
// first). Total counts all matches; it is larger than len(Hits) when the
// limit cut hits off.
type TypeHits struct {
	Type  HitType
	Hits  []Hit
	Total int
}

// SearchTyped searches every type of q.Types (TypeEvent when empty) and
// returns one group per type in that order. A reserved or unknown type
// is an error, a filter that a selected type does not apply a
// *FilterError. Without a Text and with an Attachment filter the types
// event and session list what has an attachment, newest first: the
// events (snippet = start of the text), the sessions; Text-less queries
// of other types, and without the filter, have no hits.
func (s *Store) SearchTyped(ctx context.Context, q TypedQuery) ([]TypeHits, error) {
	types := q.Types
	if len(types) == 0 {
		types = []HitType{TypeEvent}
	}
	for _, t := range types {
		if err := checkType(t); err != nil {
			return nil, fmt.Errorf("store: search: %w", err)
		}
		for _, f := range q.filters() {
			if !Supports(t, f) {
				return nil, &FilterError{Filter: f, Type: t}
			}
		}
	}
	out := make([]TypeHits, 0, len(types))
	for _, t := range types {
		d, _ := typeDefOf(t)
		g := TypeHits{Type: t, Hits: []Hit{}}
		switch {
		case strings.TrimSpace(q.Text) != "":
			var err error
			if g, err = d.search(s, ctx, q); err != nil {
				return nil, err
			}
		case q.Attachment != nil && t == TypeEvent:
			var err error
			if g, err = s.searchAttachmentEvents(ctx, q); err != nil {
				return nil, err
			}
		case q.Attachment != nil && t == TypeSession:
			var err error
			if g, err = d.search(s, ctx, q); err != nil {
				return nil, err
			}
		}
		out = append(out, g)
	}
	return out, nil
}

// total is the number of all matches: len(hits) unless the limit was
// reached, then count() runs.
func total(n, limit int, count func() (int, error)) (int, error) {
	if limit <= 0 || n < limit {
		return n, nil
	}
	return count()
}

func (s *Store) count(ctx context.Context, q string, args ...any) (int, error) {
	var n int
	if err := s.db().QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: search: %w", err)
	}
	return n, nil
}

func (s *Store) searchEventHits(ctx context.Context, q TypedQuery) (TypeHits, error) {
	g := TypeHits{Type: TypeEvent, Hits: []Hit{}}
	if q.Project != nil && len(q.Project.Keys) == 0 {
		return g, nil
	}
	hs, he := q.highlight()
	f := SearchFilter{SessionFilter: q.SessionFilter, SessionID: q.SessionID, Actor: q.Actor, Raw: q.Raw, HighlightStart: hs, HighlightEnd: he}
	if q.Project != nil {
		f.ProjectKeys = q.Project.Keys
	}
	hits, err := s.Search(ctx, q.Text, f)
	if err != nil {
		return g, err
	}
	if err := s.attachmentsOfHits(ctx, hits, q.Attachment); err != nil {
		return g, err
	}
	for i := range hits {
		h := &hits[i]
		g.Hits = append(g.Hits, Hit{Type: TypeEvent, TS: h.Event.TS, Snippet: h.Snippet, Rank: h.Rank, Project: h.Project, Event: h})
	}
	g.Total, err = total(len(hits), q.Limit, func() (int, error) {
		text := q.Text
		if !q.Raw {
			text = ftsQuery(text)
		}
		cond, args := f.where(text)
		return s.count(ctx, `SELECT count(*) FROM events_fts
			JOIN events e ON e.seq = events_fts.rowid
			JOIN sessions s ON s.id = e.session_id
			LEFT JOIN sessions p ON p.id = s.parent_id
			WHERE `+strings.Join(cond, " AND "), args[2:]...)
	})
	return g, err
}

// sessionNameExpr is what the session search matches: name, AI title
// and agent name.
const sessionNameExpr = `(COALESCE(s.name, '') || ' ' || COALESCE(s.ai_title, '') || ' ' || COALESCE(s.agent_name, ''))`

// searchSessionHits finds sessions whose name, AI title or agent name
// contain every word of the query (case-insensitive for ASCII), newest
// first. The snippet is the first of these fields with all words, the
// display name otherwise, with the words highlighted.
func (s *Store) searchSessionHits(ctx context.Context, q TypedQuery) (TypeHits, error) {
	g := TypeHits{Type: TypeSession, Hits: []Hit{}}
	if q.Project != nil && len(q.Project.Keys) == 0 {
		return g, nil
	}
	terms := strings.Fields(q.Text)
	cond, args := q.where(false)
	for _, t := range terms {
		cond = append(cond, sessionNameExpr+` LIKE ? ESCAPE '\'`)
		args = append(args, likeArg(t))
	}
	if q.SessionID != "" {
		cond = append(cond, "s.id = ?")
		args = append(args, q.SessionID)
	}
	if q.Project != nil {
		c, a := inCond(projectKeyExpr, q.Project.Keys)
		cond = append(cond, "s.id IN (SELECT s.id FROM sessions s LEFT JOIN sessions p ON p.id = s.parent_id WHERE "+c+")")
		args = append(args, a...)
	}
	list, err := s.querySessions(ctx, cond, args, q.Limit)
	if err != nil {
		return g, fmt.Errorf("store: search: %w", err)
	}
	if err := s.sessionsAttachments(ctx, list, q.Attachment); err != nil {
		return g, err
	}
	refs, err := s.projectRefs(ctx, list)
	if err != nil {
		return g, err
	}
	hs, he := q.highlight()
	for i := range list {
		si := &list[i]
		g.Hits = append(g.Hits, Hit{Type: TypeSession, TS: si.StartedAt, Snippet: highlight(sessionSnippet(*si, terms), terms, hs, he),
			Project: refs[si.ID], Session: si})
	}
	g.Total, err = total(len(list), q.Limit, func() (int, error) {
		return s.count(ctx, "SELECT count(*) FROM sessions s WHERE "+strings.Join(cond, " AND "), args...)
	})
	return g, err
}

// projectRefs loads the raw project key of every session in list.
func (s *Store) projectRefs(ctx context.Context, list []SessionInfo) (map[string]ProjectRef, error) {
	out := make(map[string]ProjectRef, len(list))
	if len(list) == 0 {
		return out, nil
	}
	ids := make([]string, len(list))
	for i, si := range list {
		ids[i] = si.ID
	}
	c, args := inCond("s.id", ids)
	rows, err := s.db().QueryContext(ctx, "SELECT s.id, "+projectCols+" FROM sessions s LEFT JOIN sessions p ON p.id = s.parent_id WHERE "+c, args...)
	if err != nil {
		return nil, fmt.Errorf("store: search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var r ProjectRef
		if err := rows.Scan(&id, &r.Key, &r.Dir, &r.Remote); err != nil {
			return nil, fmt.Errorf("store: search: %w", err)
		}
		out[id] = r.clean()
	}
	return out, rows.Err()
}

// sessionSnippet is the first of name, AI title and agent name that
// contains every term, else the display name.
func sessionSnippet(si SessionInfo, terms []string) string {
	for _, f := range []string{si.Name, si.AITitle, si.AgentName} {
		if f == "" {
			continue
		}
		low, all := strings.ToLower(f), true
		for _, t := range terms {
			if !strings.Contains(low, strings.ToLower(t)) {
				all = false
				break
			}
		}
		if all {
			return f
		}
	}
	return si.DisplayName()
}

// highlight wraps every case-insensitive occurrence of a term in hs/he.
func highlight(text string, terms []string, hs, he string) string {
	var alts []string
	for _, t := range terms {
		if t != "" {
			alts = append(alts, regexp.QuoteMeta(t))
		}
	}
	if len(alts) == 0 {
		return text
	}
	re := regexp.MustCompile("(?i)" + strings.Join(alts, "|"))
	return re.ReplaceAllStringFunc(text, func(m string) string { return hs + m + he })
}

func (s *Store) searchMemoryHits(ctx context.Context, q TypedQuery) (TypeHits, error) {
	g := TypeHits{Type: TypeMemory, Hits: []Hit{}}
	o := MemorySearchOptions{Raw: q.Raw, Limit: q.Limit, Since: q.Since, Until: q.Until, Repo: q.Repo, Harness: q.Harness}
	o.HighlightStart, o.HighlightEnd = q.highlight()
	if q.Project != nil {
		if len(q.Project.MemoryProjectIDs) == 0 {
			return g, nil
		}
		o.ProjectIDs = q.Project.MemoryProjectIDs
	}
	hits, err := s.SearchMemory(ctx, q.Text, o)
	if err != nil {
		return g, err
	}
	for i := range hits {
		h := &hits[i]
		g.Hits = append(g.Hits, Hit{Type: TypeMemory, TS: h.Changed, Snippet: h.Snippet, Rank: h.Rank, Project: h.Project, Memory: h})
	}
	g.Total, err = total(len(hits), q.Limit, func() (int, error) {
		o.Limit = 0
		all, err := s.SearchMemory(ctx, q.Text, o)
		return len(all), err
	})
	return g, err
}
