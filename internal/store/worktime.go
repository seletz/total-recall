package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// HumanEventsFilter selects the range of HumanEvents.
type HumanEventsFilter struct {
	// Since/Until select events by time in [Since, Until); a zero bound is
	// open.
	Since, Until time.Time
}

// HumanEvent is one own message (actor human) with its session and
// project, as the working-time estimate needs it (internal/worktime).
type HumanEvent struct {
	Time      time.Time
	SessionID string
	Title     string // display name of the session, "" without
	// Project is the key of the activity dimension project: the session's
	// git root, else its working directory (Dir is then true); a subagent
	// resolves to its parent like ActivitySeries.
	Project   string
	Dir       bool
	RemoteURL string // normalized remote URL, "" without a repository
	Text      string // the message, "" without text
}

// HumanEvents returns the human events of the range in time order with
// their text, session title and project. Agent, peer, automation and tool
// events are left out.
func (s *Store) HumanEvents(ctx context.Context, f HumanEventsFilter) ([]HumanEvent, error) {
	cond := []string{"e.actor = 'human'"}
	var args []any
	if !f.Since.IsZero() {
		cond = append(cond, "e.ts >= ?")
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		cond = append(cond, "e.ts < ?")
		args = append(args, formatTime(f.Until))
	}
	rows, err := s.db().QueryContext(ctx, `SELECT e.ts, e.session_id, COALESCE(e.text, ''), COALESCE(s.display_name, ''),
			COALESCE(s.parent_id, ''), COALESCE(s.cwd, ''), COALESCE(s.git_root, ''), COALESCE(s.git_remote_url, ''),
			COALESCE(p.cwd, ''), COALESCE(p.git_root, ''), COALESCE(p.git_remote_url, '')
		FROM events e
		JOIN sessions s ON s.id = e.session_id
		LEFT JOIN sessions p ON p.id = s.parent_id
		WHERE `+strings.Join(cond, " AND ")+`
		ORDER BY e.ts, e.seq`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: human events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []HumanEvent
	for rows.Next() {
		var tsv sql.NullString
		var session, text, title, parentID, cwd, gitRoot, remote, pcwd, pgitRoot, premote string
		if err := rows.Scan(&tsv, &session, &text, &title, &parentID, &cwd, &gitRoot, &remote, &pcwd, &pgitRoot, &premote); err != nil {
			return nil, fmt.Errorf("store: human events: %w", err)
		}
		t := parseTime(tsv)
		if t.IsZero() {
			continue
		}
		if pgitRoot != "" || pcwd != "" {
			remote = premote
		}
		key, dir := activityKey(DimProject, "", parentID, cwd, gitRoot, pcwd, pgitRoot, "", "")
		if dir {
			remote = ""
		}
		out = append(out, HumanEvent{Time: t, SessionID: session, Title: title, Project: key, Dir: dir, RemoteURL: remote, Text: text})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: human events: %w", err)
	}
	return out, nil
}
