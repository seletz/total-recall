package star

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/seletz/total-recall/internal/model"
)

// Unknown is what the star stores for a value outside the value set of its
// column, e.g. a kind that a newer total-recall writes.
const Unknown = "unknown"

// valueSet is the closed value set of one archive column as ingest writes
// it ("" stands for NULL). models/staging/stg_event.sql maps the same
// lists, tests/accepted_values.sql accepts them plus Unknown; a test keeps
// all three in step with internal/model.
type valueSet struct {
	Column string
	Values []string
}

var valueSets = []valueSet{
	{"actor", []string{string(model.ActorHuman), string(model.ActorAgent), string(model.ActorPeer), string(model.ActorAutomation), string(model.ActorTool), ""}},
	{"input_kind", []string{string(model.InputTyped), string(model.InputPasted), string(model.InputBash), string(model.InputRemote), ""}},
	{"kind", []string{string(model.KindMessage), string(model.KindToolUse), string(model.KindToolResult), string(model.KindSystem), string(model.KindMeta)}},
}

// UnknownValue is a value of the archive outside its column's value set:
// its events are loaded with Unknown in that column.
type UnknownValue struct {
	Column string `json:"column"`
	Value  string `json:"value"`
	Events int    `json:"events"`
}

// String is the warning of the build output.
func (u UnknownValue) String() string {
	return fmt.Sprintf("%d events with %s %q, loaded as %s", u.Events, u.Column, u.Value, Unknown)
}

// unknownValues lists the values outside the value sets among the new
// events of the attached archive (seq > from), by column and value.
func unknownValues(ctx context.Context, q querier, from int64) ([]UnknownValue, error) {
	var out []UnknownValue
	for _, s := range valueSets {
		quoted := make([]string, len(s.Values))
		for i, v := range s.Values {
			quoted[i] = "'" + sqlText(v) + "'"
		}
		query := fmt.Sprintf(`SELECT COALESCE(%[1]s, '') AS v, count(*) FROM %[2]s.main.events
WHERE seq > ? AND COALESCE(%[1]s, '') NOT IN (%[3]s) AND TRY_CAST(ts AS TIMESTAMPTZ) IS NOT NULL
GROUP BY v ORDER BY v`, s.Column, sourceAlias, strings.Join(quoted, ", "))
		rows, err := q.QueryContext(ctx, query, from)
		if err != nil {
			return nil, fmt.Errorf("values of %s: %w", s.Column, err)
		}
		for rows.Next() {
			u := UnknownValue{Column: s.Column}
			if err := rows.Scan(&u.Value, &u.Events); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out = append(out, u)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// querier runs queries that return rows.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
