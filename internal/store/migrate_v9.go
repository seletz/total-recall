package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// migrateTokensV9 is the Go part of migration 9: for every event that has
// token counts (tokens_in set) and a raw line, it reads the usage of the
// API message from raw and sets tokens_cache_read and tokens_cache_write.
// tokens_in is not touched. Events without raw (dropped for size, or
// archived before raw was complete) and raw lines without a usage stay NULL.
func (s *Store) migrateTokensV9(ctx context.Context, tx *sql.Tx, report func(done, total int64)) error {
	var total int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM events WHERE tokens_in IS NOT NULL AND raw IS NOT NULL").Scan(&total); err != nil {
		return fmt.Errorf("count events: %w", err)
	}
	upd, err := tx.PrepareContext(ctx, "UPDATE events SET tokens_cache_read = ?, tokens_cache_write = ? WHERE seq = ?")
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer func() { _ = upd.Close() }()

	type row struct {
		seq int64
		raw []byte
	}
	var done, last int64
	report(0, total)
	for {
		rows, err := tx.QueryContext(ctx, `SELECT seq, raw FROM events
			WHERE seq > ? AND tokens_in IS NOT NULL AND raw IS NOT NULL ORDER BY seq LIMIT ?`, last, migrateBatch)
		if err != nil {
			return fmt.Errorf("read events: %w", err)
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.seq, &r.raw); err != nil {
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
			var rec struct {
				Message struct {
					Usage *struct {
						CacheCreation int64 `json:"cache_creation_input_tokens"`
						CacheRead     int64 `json:"cache_read_input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			// A line that does not parse or has no usage (another harness)
			// stays NULL: unknown, not zero.
			if json.Unmarshal([]byte(line), &rec) != nil || rec.Message.Usage == nil {
				continue
			}
			if _, err := upd.ExecContext(ctx, rec.Message.Usage.CacheRead, rec.Message.Usage.CacheCreation, r.seq); err != nil {
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
