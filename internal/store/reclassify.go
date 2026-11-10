package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/seletz/total-recall/internal/model"
)

// UserMessage is a stored user message as seen by ingest --reclassify: the
// fields a harness needs to derive its actor again.
type UserMessage struct {
	ID        string
	SessionID string
	TS        time.Time
	Actor     model.Actor
	Text      string
	InputKind model.InputKind
	Raw       string // "" when raw was not stored or dropped for size
	Subagent  bool   // the session is a subagent transcript (has a parent)
	// Cwd and GitBranch are the session's working directory and (last seen)
	// branch; "" when unknown.
	Cwd, GitBranch string
	// EarlierPrompt says that the session has an earlier user message that
	// is a prompt: actor human, or automation that is no harness plumbing
	// (text not starting with "<"), such as a prompt another session sent.
	EarlierPrompt bool
}

// UserMessages calls fn for every user message (role user, kind message)
// of the harness in insertion order. It stops at the first error fn returns.
func (s *Store) UserMessages(ctx context.Context, harness string, fn func(UserMessage) error) error {
	return s.UserMessagesBetween(ctx, harness, time.Time{}, time.Time{}, fn)
}

// UserMessagesBetween is UserMessages limited to the messages with
// from <= ts <= to; a zero bound leaves that side open.
func (s *Store) UserMessagesBetween(ctx context.Context, harness string, from, to time.Time, fn func(UserMessage) error) error {
	q := `SELECT e.id, e.session_id, e.ts, COALESCE(e.actor, ''), COALESCE(e.input_kind, ''), e.text, e.raw,
			COALESCE(s.parent_id, '') <> '', COALESCE(s.cwd, ''), COALESCE(s.git_branch, ''),
			EXISTS (SELECT 1 FROM events p WHERE p.session_id = e.session_id AND p.role = 'user' AND p.kind = 'message'
				AND p.seq < e.seq AND (p.actor = 'human' OR (p.actor = 'automation' AND p.text NOT LIKE '<%')))
		FROM events e JOIN sessions s ON s.id = e.session_id
		WHERE s.harness = ? AND e.role = 'user' AND e.kind = 'message'`
	args := []any{harness}
	if !from.IsZero() {
		q += " AND e.ts >= ?"
		args = append(args, formatTime(from))
	}
	if !to.IsZero() {
		q += " AND e.ts <= ?"
		args = append(args, formatTime(to))
	}
	rows, err := s.db().QueryContext(ctx, q+" ORDER BY e.seq", args...)
	if err != nil {
		return fmt.Errorf("store: user messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var m UserMessage
		var actor, ik string
		var ts sql.NullString
		var raw []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &ts, &actor, &ik, &m.Text, &raw, &m.Subagent, &m.Cwd, &m.GitBranch, &m.EarlierPrompt); err != nil {
			return fmt.Errorf("store: user messages: %w", err)
		}
		m.TS = parseTime(ts)
		if m.Raw, err = rawText(raw); err != nil {
			return fmt.Errorf("store: user messages: %s: %w", m.ID, err)
		}
		m.Actor, m.InputKind = model.Actor(actor), model.InputKind(ik)
		if err := fn(m); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: user messages: %w", err)
	}
	return nil
}

// ToolCall is the argument text of a tool_use event.
type ToolCall struct {
	SessionID string
	TS        time.Time
	Text      string
}

// ToolCalls lists the tool calls with from <= ts <= to, oldest first. The
// text is the argument as stored (cut at ingest.tool_input_max).
func (s *Store) ToolCalls(ctx context.Context, from, to time.Time) ([]ToolCall, error) {
	q := `SELECT session_id, ts, COALESCE(text, '') FROM events WHERE kind = 'tool_use'`
	var args []any
	if !from.IsZero() {
		q += " AND ts >= ?"
		args = append(args, formatTime(from))
	}
	if !to.IsZero() {
		q += " AND ts <= ?"
		args = append(args, formatTime(to))
	}
	rows, err := s.db().QueryContext(ctx, q+" ORDER BY ts, seq", args...)
	if err != nil {
		return nil, fmt.Errorf("store: tool calls: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ToolCall
	for rows.Next() {
		var c ToolCall
		var ts sql.NullString
		if err := rows.Scan(&c.SessionID, &ts, &c.Text); err != nil {
			return nil, fmt.Errorf("store: tool calls: %w", err)
		}
		c.TS = parseTime(ts)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: tool calls: %w", err)
	}
	return out, nil
}

// ActorChange sets the actor and the input kind of one event. InputKind is
// stored as it is, "" meaning NULL (unknown or not a human input).
type ActorChange struct {
	ID        string
	Actor     model.Actor
	InputKind model.InputKind
}

// SetEventActors applies the changes in one transaction and returns how
// many rows actually changed (an unknown ID or an unchanged actor and input
// kind counts nothing). It is used by ingest --reclassify.
func (s *Store) SetEventActors(ctx context.Context, changes []ActorChange) (int64, error) {
	if s.readOnly {
		return 0, fmt.Errorf("store: read-only")
	}
	for _, c := range changes {
		if !c.Actor.Valid() {
			return 0, fmt.Errorf("store: set event actor %s: invalid actor %q", c.ID, c.Actor)
		}
	}
	if len(changes) == 0 {
		return 0, nil
	}
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `UPDATE events SET actor = ?, input_kind = ? WHERE id = ? AND (actor IS NOT ? OR input_kind IS NOT ?)`)
	if err != nil {
		return 0, fmt.Errorf("store: prepare: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	var n int64
	for _, c := range changes {
		res, err := stmt.ExecContext(ctx, string(c.Actor), nullStr(string(c.InputKind)), c.ID, string(c.Actor), nullStr(string(c.InputKind)))
		if err != nil {
			return 0, fmt.Errorf("store: set event actor %s: %w", c.ID, err)
		}
		k, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("store: set event actor %s: %w", c.ID, err)
		}
		n += k
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit: %w", err)
	}
	return n, nil
}
