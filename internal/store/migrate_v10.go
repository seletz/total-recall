package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/seletz/total-recall/internal/inputkind"
)

// migrateInputKindV10 is the Go part of migration 10: for every human user
// message of the claude harness it derives events.input_kind from the text
// and the raw line (inputkind.FromRaw). Without a raw line only bash and
// pasted are decidable from the text; every other event stays NULL
// (unknown), as do events of other harnesses and of other actors.
func (s *Store) migrateInputKindV10(ctx context.Context, tx *sql.Tx, report func(done, total int64)) error {
	const where = `e.actor = 'human' AND e.role = 'user' AND e.kind = 'message'
		AND e.session_id IN (SELECT id FROM sessions WHERE harness = 'claude')`
	var total int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM events e WHERE "+where).Scan(&total); err != nil {
		return fmt.Errorf("count events: %w", err)
	}
	// Only input_kind changes: the UPDATE OF text trigger must not fire.
	upd, err := tx.PrepareContext(ctx, "UPDATE events SET input_kind = ? WHERE seq = ?")
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer func() { _ = upd.Close() }()

	type row struct {
		seq  int64
		text string
		raw  []byte
	}
	var done, last int64
	report(0, total)
	for {
		rows, err := tx.QueryContext(ctx, `SELECT e.seq, e.text, e.raw FROM events e
			WHERE e.seq > ? AND `+where+` ORDER BY e.seq LIMIT ?`, last, migrateBatch)
		if err != nil {
			return fmt.Errorf("read events: %w", err)
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.seq, &r.text, &r.raw); err != nil {
				_ = rows.Close()
				return fmt.Errorf("read events: %w", err)
			}
			batch = append(batch, r)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("read events: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, r := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			last = r.seq
			line, err := rawText(r.raw)
			if err != nil {
				return fmt.Errorf("event seq %d: %w", r.seq, err)
			}
			kind, ok := inputkind.FromRaw(r.text, line)
			if !ok {
				continue
			}
			if _, err := upd.ExecContext(ctx, string(kind), r.seq); err != nil {
				return fmt.Errorf("update event seq %d: %w", r.seq, err)
			}
		}
		done += int64(len(batch))
		report(done, total)
		if len(batch) < migrateBatch {
			break
		}
	}
	return nil
}
