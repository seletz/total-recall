package store

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Attachment filter (#272): sessions and events that carry attachments,
// the images and documents of event_blobs. The same SessionFilter field
// works for Sessions, for the event and session hits of SearchTyped and
// for SearchBySession; the TUI uses it as is. An attachment is an image
// by its media type (image/…), everything else in event_blobs a
// document, so image and document together are all of them.

// AttachmentKind selects images or documents.
type AttachmentKind string

const (
	// AttachmentAny takes every attachment.
	AttachmentAny AttachmentKind = ""
	// AttachmentImage takes the media types image/….
	AttachmentImage AttachmentKind = "image"
	// AttachmentDocument takes every media type that is no image (PDF…).
	AttachmentDocument AttachmentKind = "document"
)

// ParseAttachmentKind parses a kind name: "" and "any" are every
// attachment, "image" and "document" the kinds. Anything else is an
// error that names the valid values.
func ParseAttachmentKind(s string) (AttachmentKind, error) {
	switch s {
	case "", "any":
		return AttachmentAny, nil
	case string(AttachmentImage):
		return AttachmentImage, nil
	case string(AttachmentDocument):
		return AttachmentDocument, nil
	}
	return "", fmt.Errorf("unknown attachment kind %q (any, image, document)", s)
}

// Matches reports whether an attachment of the media type is of the kind.
func (k AttachmentKind) Matches(mediaType string) bool {
	isImage := strings.HasPrefix(mediaType, "image/")
	switch k {
	case AttachmentImage:
		return isImage
	case AttachmentDocument:
		return !isImage
	}
	return true
}

// sql is the condition on the blob alias "ay", "" for every kind.
func (k AttachmentKind) sql() string {
	switch k {
	case AttachmentImage:
		return "ay.media_type LIKE 'image/%'"
	case AttachmentDocument:
		return "ay.media_type NOT LIKE 'image/%'"
	}
	return ""
}

// AttachmentFilter selects what carries an attachment: a nil
// *AttachmentFilter in SessionFilter does not filter. For events (the
// event hits of a search) it keeps the events with an attachment of the
// Kind; for sessions the sessions with such an event (whatever the
// time window of the events, Since and Until select the sessions as
// usual). Actor, when set, is the actor of the event with the
// attachment (human for what the user pasted, tool for the screenshots
// of tool results).
type AttachmentFilter struct {
	Kind  AttachmentKind
	Actor string
}

// blobJoin and kind condition of a filter, for the aliases "ab"
// (event_blobs) and "ay" (blobs).
func (a AttachmentFilter) joinAndCond() (join, cond string) {
	if c := a.Kind.sql(); c != "" {
		return " JOIN blobs ay ON ay.sha256 = ab.sha256", " AND " + c
	}
	return "", ""
}

// eventCond is the condition on the event alias "e": it has an
// attachment of the kind (and the actor).
func (a AttachmentFilter) eventCond() (string, []any) {
	join, cond := a.joinAndCond()
	c := "EXISTS (SELECT 1 FROM event_blobs ab" + join + " WHERE ab.event_id = e.id" + cond + ")"
	var args []any
	if a.Actor != "" {
		c += " AND e.actor = ?"
		args = append(args, a.Actor)
	}
	return c, args
}

// sessionCond is the condition on the session alias "s": one of its
// events has an attachment of the kind (and the actor).
func (a AttachmentFilter) sessionCond() (string, []any) {
	join, cond := a.joinAndCond()
	c := "EXISTS (SELECT 1 FROM events ae JOIN event_blobs ab ON ab.event_id = ae.id" + join + " WHERE ae.session_id = s.id" + cond
	var args []any
	if a.Actor != "" {
		c += " AND ae.actor = ?"
		args = append(args, a.Actor)
	}
	return c + ")", args
}

// filterAttachments keeps the attachments of the kind.
func filterAttachments(list []EventAttachment, k AttachmentKind) []EventAttachment {
	if k == AttachmentAny {
		return list
	}
	var out []EventAttachment
	for _, a := range list {
		if k.Matches(a.MediaType) {
			out = append(out, a)
		}
	}
	return out
}

// attachmentsOfHits loads the attachments of the hit events, those of the
// kind (every kind for a nil filter), into SearchHit.Attachments.
func (s *Store) attachmentsOfHits(ctx context.Context, hits []SearchHit, f *AttachmentFilter) error {
	ids := make([]string, len(hits))
	for i := range hits {
		ids[i] = hits[i].Event.ID
	}
	by, err := s.EventsAttachments(ctx, ids)
	if err != nil {
		return err
	}
	var kind AttachmentKind
	if f != nil {
		kind = f.Kind
	}
	for i := range hits {
		hits[i].Attachments = filterAttachments(by[hits[i].Event.ID], kind)
	}
	return nil
}

// sessionsAttachments sets SessionInfo.Attachments of every session of
// list: the number of its attachments of the kind and actor of f (all of
// them for a nil filter). One query per batch over event_blobs, which is
// small compared to the events.
func (s *Store) sessionsAttachments(ctx context.Context, list []SessionInfo, f *AttachmentFilter) error {
	if len(list) == 0 {
		return nil
	}
	join, cond := "", ""
	actor := ""
	if f != nil {
		join, cond = f.joinAndCond()
		actor = f.Actor
	}
	counts := map[string]int{}
	for start := 0; start < len(list); start += eventsAttachmentsBatch {
		batch := list[start:min(start+eventsAttachmentsBatch, len(list))]
		ids := make([]string, len(batch))
		for i := range batch {
			ids[i] = batch[i].ID
		}
		in, args := inCond("e.session_id", ids)
		q := "SELECT e.session_id, count(*) FROM event_blobs ab JOIN events e ON e.id = ab.event_id" + join + " WHERE " + in + cond
		if actor != "" {
			q += " AND e.actor = ?"
			args = append(args, actor)
		}
		rows, err := s.db().QueryContext(ctx, q+" GROUP BY e.session_id", args...)
		if err != nil {
			return fmt.Errorf("store: session attachments: %w", err)
		}
		for rows.Next() {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				_ = rows.Close()
				return fmt.Errorf("store: session attachments: %w", err)
			}
			counts[id] = n
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return fmt.Errorf("store: session attachments: %w", err)
		}
	}
	for i := range list {
		list[i].Attachments = counts[list[i].ID]
	}
	return nil
}

// snippetRunes is the length of the snippet of an attachment hit, which
// has no search word to cut around.
const snippetRunes = 160

// textStart is the first line-wise squeezed runes of text.
func textStart(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= snippetRunes {
		return text
	}
	r := []rune(text)
	return string(r[:snippetRunes]) + "…"
}

// searchAttachmentEvents lists the events with attachments without a
// search word, newest first: the snippet is the start of the text. The
// filters of q apply as for a word search.
func (s *Store) searchAttachmentEvents(ctx context.Context, q TypedQuery) (TypeHits, error) {
	g := TypeHits{Type: TypeEvent, Hits: []Hit{}}
	if q.Project != nil && len(q.Project.Keys) == 0 {
		return g, nil
	}
	f := SearchFilter{SessionFilter: q.SessionFilter, SessionID: q.SessionID, Actor: q.Actor}
	if q.Project != nil {
		f.ProjectKeys = q.Project.Keys
	}
	cond, args := f.eventCond()
	where := " WHERE " + strings.Join(cond, " AND ")
	from := ` FROM events e
		LEFT JOIN sources src ON src.id = e.source_id
		JOIN sessions s ON s.id = e.session_id
		LEFT JOIN sessions p ON p.id = s.parent_id`
	query := "SELECT " + eventCols + `, COALESCE(NULLIF(s.name, ''), NULLIF(s.ai_title, ''), NULLIF(s.agent_name, ''), s.native_id, s.id), ` +
		repoExpr + ", " + projectCols + from + where + " ORDER BY e.ts DESC, e.seq DESC"
	if q.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", q.Limit)
	}
	rows, err := s.db().QueryContext(ctx, query, args...)
	if err != nil {
		return g, fmt.Errorf("store: search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var hits []SearchHit
	for rows.Next() {
		var h SearchHit
		e, err := scanEvent(rows, &h.SessionName, &h.Repo, &h.Project.Key, &h.Project.Dir, &h.Project.Remote)
		if err != nil {
			return g, fmt.Errorf("store: search: %w", err)
		}
		h.Event, h.Project, h.Snippet = e, h.Project.clean(), textStart(e.Text)
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return g, fmt.Errorf("store: search: %w", err)
	}
	if err := s.attachmentsOfHits(ctx, hits, q.Attachment); err != nil {
		return g, err
	}
	for i := range hits {
		h := &hits[i]
		g.Hits = append(g.Hits, Hit{Type: TypeEvent, TS: h.Event.TS, Snippet: h.Snippet, Project: h.Project, Event: h})
	}
	g.Total, err = total(len(hits), q.Limit, func() (int, error) {
		return s.count(ctx, "SELECT count(*)"+from+where, args...)
	})
	return g, err
}
