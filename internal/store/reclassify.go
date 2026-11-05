package store

import (
	"context"
	"fmt"

	"github.com/seletz/total-recall/internal/model"
)

// UserMessage is a stored user message as seen by ingest --reclassify: the
// fields a harness needs to derive its actor again.
type UserMessage struct {
	ID       string
	Actor    model.Actor
	Text     string
	Raw      string // "" when raw was not stored or dropped for size
	Subagent bool   // the session is a subagent transcript (has a parent)
}

// UserMessages calls fn for every user message (role user, kind message)
// of the harness in insertion order. It stops at the first error fn returns.
func (s *Store) UserMessages(ctx context.Context, harness string, fn func(UserMessage) error) error {
	rows, err := s.db().QueryContext(ctx, `SELECT e.id, COALESCE(e.actor, ''), e.text, e.raw,
			COALESCE(s.parent_id, '') <> ''
		FROM events e JOIN sessions s ON s.id = e.session_id
		WHERE s.harness = ? AND e.role = 'user' AND e.kind = 'message'
		ORDER BY e.seq`, harness)
	if err != nil {
		return fmt.Errorf("store: user messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var m UserMessage
		var actor string
		var raw []byte
		if err := rows.Scan(&m.ID, &actor, &m.Text, &raw, &m.Subagent); err != nil {
			return fmt.Errorf("store: user messages: %w", err)
		}
		if m.Raw, err = rawText(raw); err != nil {
			return fmt.Errorf("store: user messages: %s: %w", m.ID, err)
		}
		m.Actor = model.Actor(actor)
		if err := fn(m); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: user messages: %w", err)
	}
	return nil
}

// ActorChange sets the actor of one event.
type ActorChange struct {
	ID    string
	Actor model.Actor
}

// SetEventActors applies the changes in one transaction and returns how
// many rows actually changed (an unknown ID or an unchanged actor counts
// nothing). It is used by ingest --reclassify.
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
	stmt, err := tx.PrepareContext(ctx, `UPDATE events SET actor = ? WHERE id = ? AND actor IS NOT ?`)
	if err != nil {
		return 0, fmt.Errorf("store: prepare: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	var n int64
	for _, c := range changes {
		res, err := stmt.ExecContext(ctx, string(c.Actor), c.ID, string(c.Actor))
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
