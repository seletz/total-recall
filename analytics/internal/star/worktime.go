package star

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/worktime"
)

// Worktime configures the work-time fact: the settings of [worktime] and
// the start of the day, as stats --worktime uses them.
type Worktime struct {
	Settings worktime.Settings
	DayStart time.Duration
}

// workDDL creates the two work-time facts. They are computed by Go
// (worktime.Compute, the function of stats --worktime) and rebuilt on every
// run from all sources together: the time between two own prompts belongs
// to the person, not to a host, so hosts are summed before the gaps are
// credited, never after.
const workDDL = `
CREATE OR REPLACE TABLE fact_work_slice (
	local_date   DATE NOT NULL,   -- working day (day_start applied) in the primary zone
	category     VARCHAR NOT NULL,
	session_key  BIGINT NOT NULL,
	raw_s        BIGINT NOT NULL  -- credited seconds of the session on the day
);
CREATE OR REPLACE TABLE fact_work_day (
	local_date   DATE NOT NULL,
	category     VARCHAR NOT NULL,
	raw_s        BIGINT NOT NULL,
	rounded_s    BIGINT NOT NULL  -- the time to book: rounded per [worktime] grid and scope
);`

// loadWorktime computes the work-time facts from the human events of
// fact_event (every source) and writes them.
func loadWorktime(ctx context.Context, tx *sql.Tx, o Options) error {
	for _, s := range splitSQL(workDDL) {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("worktime: %w", err)
		}
	}
	if o.Worktime == nil {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT f.ts_utc, s.session_id, p.project_path, p.is_dir, p.remote_url
		FROM fact_event f
		JOIN dim_actor a ON a.actor_key = f.actor_key
		JOIN dim_session s ON s.session_key = f.session_key
		JOIN dim_project_key p ON p.project_key = f.project_key
		WHERE a.actor = 'human'
		ORDER BY f.ts_utc, f.source_seq`)
	if err != nil {
		return fmt.Errorf("worktime: %w", err)
	}
	var in []worktime.Event
	for rows.Next() {
		var ev worktime.Event
		var path string
		if err := rows.Scan(&ev.Time, &ev.Session, &path, &ev.Dir, &ev.Remote); err != nil {
			_ = rows.Close()
			return fmt.Errorf("worktime: %w", err)
		}
		ev.Time = ev.Time.UTC()
		ev.Project = untilde(path, o.Home)
		in = append(in, ev)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("worktime: %w", err)
	}
	rep := worktime.Compute(in, o.Worktime.Settings, worktime.Options{
		Location: o.TZ, DayStart: o.Worktime.DayStart, Home: o.Home, Projects: o.Projects,
	})
	var slices, days []string
	var sargs, dargs []any
	for _, d := range rep.Days {
		for _, c := range d.Categories {
			days = append(days, "(CAST(? AS DATE), ?, ?, ?)")
			dargs = append(dargs, d.Label, c.Name, int64(c.Raw/time.Second), int64(c.Rounded/time.Second))
			for _, s := range c.Sessions {
				slices = append(slices, "(CAST(? AS DATE), ?, ?, ?)")
				sargs = append(sargs, d.Label, c.Name, Key("session", s.ID), int64(s.Raw/time.Second))
			}
		}
	}
	if err := insertBatches(ctx, tx, "fact_work_slice", slices, sargs, 4); err != nil {
		return err
	}
	return insertBatches(ctx, tx, "fact_work_day", days, dargs, 4)
}

// insertBatches inserts value tuples in chunks of 500 rows.
func insertBatches(ctx context.Context, tx *sql.Tx, table string, tuples []string, args []any, width int) error {
	const chunk = 500
	for i := 0; i < len(tuples); i += chunk {
		j := min(i+chunk, len(tuples))
		q := "INSERT INTO " + table + " VALUES " + strings.Join(tuples[i:j], ", ")
		if _, err := tx.ExecContext(ctx, q, args[i*width:j*width]...); err != nil {
			return fmt.Errorf("worktime: %s: %w", table, err)
		}
	}
	return nil
}
