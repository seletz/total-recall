package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Tokens are summed token counts of the events that carry usage.
//
// Input is the whole input (the sum of events.tokens_in) and splits into
// Fresh + CacheRead + CacheWrite + Unsplit: Unsplit is the input of events
// whose cache columns are NULL (archived without raw, see migration 9), for
// which the split is unknown.
type Tokens struct {
	// Events is the number of events with token counts.
	Events int
	Input  int64
	// Fresh, CacheRead and CacheWrite are the parts of Input of the events
	// with a known split.
	Fresh, CacheRead, CacheWrite int64
	Unsplit                      int64
	// Output includes the model's thinking tokens.
	Output int64
}

func (t *Tokens) add(in, out int64, cr, cw sql.NullInt64) {
	t.Events++
	t.Input += in
	t.Output += out
	if cr.Valid && cw.Valid {
		t.CacheRead += cr.Int64
		t.CacheWrite += cw.Int64
		t.Fresh += in - cr.Int64 - cw.Int64
	} else {
		t.Unsplit += in
	}
}

// DayTokens is the token use of one local day.
type DayTokens struct {
	Day string
	Tokens
}

// ModelTokens is the token use of one model ("" for events without one).
type ModelTokens struct {
	Model string
	Tokens
}

// TokenStats are the token counts of a period.
type TokenStats struct {
	Total   Tokens
	ByDay   []DayTokens   // sorted by day
	ByModel []ModelTokens // largest input first, then model
}

// TokenStats sums the token counts of the events in f's range, in total,
// per local day (f.Location) and per model. It counts every event with
// counts, whatever its actor.
func (s *Store) TokenStats(ctx context.Context, f StatsFilter) (TokenStats, error) {
	loc := f.Location
	if loc == nil {
		loc = time.UTC
	}
	cond := []string{"(e.tokens_in IS NOT NULL OR e.tokens_out IS NOT NULL)"}
	var args []any
	if !f.Since.IsZero() {
		cond = append(cond, "e.ts >= ?")
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		cond = append(cond, "e.ts < ?")
		args = append(args, formatTime(f.Until))
	}
	rows, err := s.db().QueryContext(ctx, `SELECT e.ts, e.model, e.tokens_in, e.tokens_out, e.tokens_cache_read, e.tokens_cache_write
		FROM events e WHERE `+strings.Join(cond, " AND "), args...)
	if err != nil {
		return TokenStats{}, fmt.Errorf("store: token stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out TokenStats
	days := map[string]*Tokens{}
	models := map[string]*Tokens{}
	for rows.Next() {
		var tsv, mdl sql.NullString
		var in, o, cr, cw sql.NullInt64
		if err := rows.Scan(&tsv, &mdl, &in, &o, &cr, &cw); err != nil {
			return TokenStats{}, fmt.Errorf("store: token stats: %w", err)
		}
		t := parseTime(tsv)
		if t.IsZero() {
			continue
		}
		day := t.In(loc).Format("2006-01-02")
		if days[day] == nil {
			days[day] = &Tokens{}
		}
		if models[mdl.String] == nil {
			models[mdl.String] = &Tokens{}
		}
		out.Total.add(in.Int64, o.Int64, cr, cw)
		days[day].add(in.Int64, o.Int64, cr, cw)
		models[mdl.String].add(in.Int64, o.Int64, cr, cw)
	}
	if err := rows.Err(); err != nil {
		return TokenStats{}, fmt.Errorf("store: token stats: %w", err)
	}
	for d, t := range days {
		out.ByDay = append(out.ByDay, DayTokens{Day: d, Tokens: *t})
	}
	sort.Slice(out.ByDay, func(i, j int) bool { return out.ByDay[i].Day < out.ByDay[j].Day })
	for m, t := range models {
		out.ByModel = append(out.ByModel, ModelTokens{Model: m, Tokens: *t})
	}
	sort.Slice(out.ByModel, func(i, j int) bool {
		a, b := out.ByModel[i], out.ByModel[j]
		if a.Input != b.Input {
			return a.Input > b.Input
		}
		return a.Model < b.Model
	})
	return out, nil
}
