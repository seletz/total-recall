package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/seletz/total-recall/internal/model"
)

// TimelineFilter selects the events of a Timeline.
type TimelineFilter struct {
	// SessionFilter: Since/Until select events with Since <= ts < Until,
	// Repo, Harness, Name and Account filter by session. Limit keeps the
	// oldest Limit events (0 = no limit).
	SessionFilter
	// Actor is an exact actor (human, agent, …); empty takes every actor.
	Actor string
	// ProjectKeys restricts to sessions whose raw project key is one of
	// these; empty does not filter.
	ProjectKeys []string
	// AllKinds includes tool calls and results, system and meta events;
	// otherwise only messages (kind = message).
	AllKinds bool
}

// TimelineEvent is one event of a Timeline with its session.
type TimelineEvent struct {
	Event       model.Event
	SessionName string // name, else AI title, else agent name, else native ID
	Repo        string
	// Project is the raw project key of the event's session.
	Project ProjectRef
}

// Timeline lists events across all sessions, oldest first (ties by
// insertion order). Subagent sessions are included and belong to their
// parent's project.
func (s *Store) Timeline(ctx context.Context, f TimelineFilter) ([]TimelineEvent, error) {
	cond, args := f.where(true)
	if f.Actor != "" {
		cond = append(cond, "e.actor = ?")
		args = append(args, f.Actor)
	}
	if !f.AllKinds {
		cond = append(cond, "e.kind = 'message'")
	}
	if len(f.ProjectKeys) > 0 {
		c, a := inCond(projectKeyExpr, f.ProjectKeys)
		cond, args = append(cond, c), append(args, a...)
	}
	q := "SELECT " + eventCols + `, COALESCE(NULLIF(s.name, ''), NULLIF(s.ai_title, ''), NULLIF(s.agent_name, ''), s.native_id, s.id), ` +
		repoExpr + `, ` + projectCols + `
		FROM ` + eventFrom + `
		JOIN sessions s ON s.id = e.session_id
		LEFT JOIN sessions p ON p.id = s.parent_id`
	if len(cond) > 0 {
		q += " WHERE " + strings.Join(cond, " AND ")
	}
	q += " ORDER BY e.ts, e.seq"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: timeline: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []TimelineEvent
	for rows.Next() {
		var h TimelineEvent
		e, err := scanEvent(rows, &h.SessionName, &h.Repo, &h.Project.Key, &h.Project.Dir, &h.Project.Remote)
		if err != nil {
			return nil, fmt.Errorf("store: timeline: %w", err)
		}
		h.Event, h.Project = e, h.Project.clean()
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: timeline: %w", err)
	}
	return out, nil
}
