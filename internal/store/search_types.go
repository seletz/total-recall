package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Finer object types (#344). The event types input, answer, tool_call,
// tool_result, image and document are refinements of TypeEvent: they run
// on the same index (events_fts) and return the same hits, restricted by
// the columns kind and actor or by the attachment of the event; the type
// of the hit is the refinement. TypeFile searches the content of the
// backup copies of edited files (file history), which have no index of
// their own: the copies of the candidate sessions are read and matched
// word by word.

// eventFilters are the filters of the event search without actor: the
// kind types fix the actor themselves.
var eventFilters = []string{FilterSince, FilterUntil, FilterRepo, FilterHarness, FilterName, FilterAccount, FilterAccountGroup,
	FilterProject, FilterSession, FilterRaw, FilterAttachment}

// fileFilters are the filters of the file search: the session filters;
// the words are matched in the content, so no FTS syntax and no actor.
var fileFilters = []string{FilterSince, FilterUntil, FilterRepo, FilterHarness, FilterName, FilterAccount, FilterAccountGroup,
	FilterProject, FilterSession}

func withFilters(base []string, more ...string) []string {
	return append(slices.Clone(base), more...)
}

func without(base []string, drop string) []string {
	return slices.DeleteFunc(slices.Clone(base), func(f string) bool { return f == drop })
}

// kindSearch returns the search of the event type t.
func kindSearch(t HitType) func(*Store, context.Context, TypedQuery) (TypeHits, error) {
	return func(s *Store, ctx context.Context, q TypedQuery) (TypeHits, error) {
		return s.searchEventsAs(ctx, q, t)
	}
}

// isEventType reports whether t searches events: TypeEvent and its
// refinements.
func isEventType(t HitType) bool {
	switch t {
	case TypeEvent, TypeInput, TypeAnswer, TypeToolCall, TypeToolResult, TypeImage, TypeDocument:
		return true
	}
	return false
}

// eventFilterOf translates the type t into the SearchFilter of the event
// search and returns the query with the attachment filter the type
// implies (image and document).
func eventFilterOf(q TypedQuery, t HitType) (SearchFilter, TypedQuery) {
	f := SearchFilter{SessionFilter: q.SessionFilter, SessionID: q.SessionID, Actor: q.Actor, Tool: q.Tool}
	if q.Project != nil {
		f.ProjectKeys = q.Project.Keys
	}
	switch t {
	case TypeInput:
		f.Actor, f.Kind = "human", "message"
	case TypeAnswer:
		f.Actor, f.Kind = "agent", "message"
	case TypeToolCall:
		f.Kind = "tool_use"
	case TypeToolResult:
		f.Kind = "tool_result"
	case TypeImage, TypeDocument:
		kind := AttachmentImage
		if t == TypeDocument {
			kind = AttachmentDocument
		}
		// The actor is the one of the event that carries the attachment.
		q.Attachment = &AttachmentFilter{Kind: kind, Actor: q.Actor}
		f.Actor, f.Attachment = "", q.Attachment
	}
	if t != TypeToolCall {
		f.Tool = ""
	}
	return f, q
}

// FileHit is a backup copy of an edited file (type file).
type FileHit struct {
	SessionID, SessionName, Repo string
	// MetaID is the file-history record that names the copy.
	MetaID string
	// Path is the path of the edited file as the record names it, empty
	// when the record does not say.
	Path string
	// SHA256, MediaType and Bytes describe the copy (blob).
	SHA256, MediaType string
	Bytes             int64
}

type fileCandidate struct {
	metaID, sessionID, sessionName, repo, sha, mediaType string
	ts                                                   time.Time
	bytes                                                int64
	project                                              ProjectRef
}

// searchFileHits finds the backup copies (text) of the file history whose
// content has every word of the query (case-insensitive), newest first.
// The snippet is the text around the first word, the highlighted words
// included.
func (s *Store) searchFileHits(ctx context.Context, q TypedQuery) (TypeHits, error) {
	g := TypeHits{Type: TypeFile, Hits: []Hit{}}
	if q.Project != nil && len(q.Project.Keys) == 0 {
		return g, nil
	}
	terms := strings.Fields(q.Text)
	res := make([]*regexp.Regexp, len(terms))
	for i, t := range terms {
		res[i] = regexp.MustCompile("(?i)" + regexp.QuoteMeta(t))
	}
	cond, args := q.where(true)
	cond = append([]string{"e.type IN ('file-history-snapshot', 'file-history-delta')", "b.media_type = 'text/plain'"}, cond...)
	if q.SessionID != "" {
		cond = append(cond, "e.session_id = ?")
		args = append(args, q.SessionID)
	}
	if q.Project != nil {
		c, a := inCond(projectKeyExpr, q.Project.Keys)
		cond, args = append(cond, c), append(args, a...)
	}
	// session_meta is aliased "e" so that the session filter, written for
	// events, applies to the time of the record.
	rows, err := s.db().QueryContext(ctx, `SELECT e.id, e.session_id, COALESCE(e.ts, ''),
			COALESCE(NULLIF(s.name, ''), NULLIF(s.ai_title, ''), NULLIF(s.agent_name, ''), s.native_id, s.id), `+repoExpr+`,
			mb.sha256, b.media_type, b.bytes, `+projectCols+`
		FROM meta_blobs mb
		JOIN session_meta e ON e.id = mb.meta_id
		JOIN blobs b ON b.sha256 = mb.sha256
		JOIN sessions s ON s.id = e.session_id
		LEFT JOIN sessions p ON p.id = s.parent_id
		WHERE `+strings.Join(cond, " AND ")+` ORDER BY e.ts DESC, e.seq DESC, mb.position`, args...)
	if err != nil {
		return g, fmt.Errorf("store: search: %w", err)
	}
	var cands []fileCandidate
	for rows.Next() {
		var c fileCandidate
		var tsv string
		if err := rows.Scan(&c.metaID, &c.sessionID, &tsv, &c.sessionName, &c.repo, &c.sha, &c.mediaType, &c.bytes,
			&c.project.Key, &c.project.Dir, &c.project.Remote); err != nil {
			_ = rows.Close()
			return g, fmt.Errorf("store: search: %w", err)
		}
		c.ts, c.project = parseTime(sql.NullString{String: tsv, Valid: true}), c.project.clean()
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return g, fmt.Errorf("store: search: %w", err)
	}
	_ = rows.Close()
	hs, he := q.highlight()
	// The content is matched once per hash: copies repeat across records.
	matched := map[string]string{}
	for _, c := range cands {
		snip, seen := matched[c.sha]
		if !seen {
			_, data, err := s.Blob(ctx, c.sha)
			if err != nil {
				return g, fmt.Errorf("store: search: %w", err)
			}
			snip = fileSnippet(string(data), res)
			matched[c.sha] = snip
		}
		if snip == "" {
			continue
		}
		g.Total++
		if q.Limit > 0 && len(g.Hits) >= q.Limit {
			continue
		}
		path, err := s.fileHistoryPath(ctx, c.metaID, c.sha)
		if err != nil {
			return g, err
		}
		g.Hits = append(g.Hits, Hit{Type: TypeFile, TS: c.ts, Snippet: highlight(snip, terms, hs, he), Project: c.project,
			File: &FileHit{SessionID: c.sessionID, SessionName: c.sessionName, Repo: c.repo, MetaID: c.metaID, Path: path,
				SHA256: c.sha, MediaType: c.mediaType, Bytes: c.bytes}})
	}
	return g, nil
}

// fileSnippet returns "" unless text has every word; else the text
// around the first one, on a single line.
func fileSnippet(text string, res []*regexp.Regexp) string {
	first := -1
	for _, re := range res {
		loc := re.FindStringIndex(text)
		if loc == nil {
			return ""
		}
		if first < 0 || loc[0] < first {
			first = loc[0]
		}
	}
	const ctx = 60
	start, end := first, first
	for n := 0; start > 0 && n < ctx; n++ {
		_, w := utf8.DecodeLastRuneInString(text[:start])
		start -= w
	}
	for n := 0; end < len(text) && n < 2*ctx; n++ {
		_, w := utf8.DecodeRuneInString(text[end:])
		end += w
	}
	out := strings.Join(strings.Fields(text[start:end]), " ")
	if start > 0 {
		out = "…" + out
	}
	if end < len(text) {
		out += "…"
	}
	return out
}

// fileHistoryPath is the path of the file whose copy has the hash sha in
// the file-history record metaID: the trackingPath of a delta, the key of
// the backup object in a snapshot.
func (s *Store) fileHistoryPath(ctx context.Context, metaID, sha string) (string, error) {
	var b []byte
	if err := s.db().QueryRowContext(ctx, `SELECT raw FROM session_meta WHERE id = ?`, metaID).Scan(&b); err != nil {
		return "", fmt.Errorf("store: search: %w", err)
	}
	raw, err := rawText(b)
	if err != nil {
		return "", fmt.Errorf("store: search: %w", err)
	}
	var v map[string]any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return "", nil
	}
	if p, ok := v["trackingPath"].(string); ok && p != "" {
		return p, nil
	}
	return pathOfBlob(v, sha), nil
}

// pathOfBlob finds the key under which an object refers to the blob sha.
func pathOfBlob(v any, sha string) string {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			if m, ok := c.(map[string]any); ok {
				if ref, _ := m["blob"].(string); strings.HasSuffix(ref, sha) {
					return k
				}
			}
			if p := pathOfBlob(c, sha); p != "" {
				return p
			}
		}
	case []any:
		for _, c := range x {
			if p := pathOfBlob(c, sha); p != "" {
				return p
			}
		}
	}
	return ""
}
