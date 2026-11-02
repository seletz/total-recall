package cli

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
)

// ActivitySeriesJSON is the --json output of stats --period/--by: activity
// per period and, with --by, per dimension value. Without --by, By is "",
// Keys and every Cells are empty.
type ActivitySeriesJSON struct {
	Since    string `json:"since"`
	Until    string `json:"until"`
	Timezone string `json:"timezone"`
	Period   string `json:"period"` // day, week or month
	By       string `json:"by"`     // project, agent, directory or ""
	// Keys are the dimension values in the order of the cells, by volume
	// descending; "other" (--top) is last. For --by project these are the
	// project names, else the raw keys.
	Keys    []string             `json:"keys"`
	Buckets []ActivityBucketJSON `json:"buckets"` // every period of the range, empty ones included
}

// ActivityBucketJSON is one period: its totals and, with --by, the cells
// with activity (a key without activity in the period has no cell).
type ActivityBucketJSON struct {
	Start string `json:"start"` // start of the period, UTC
	Label string `json:"label"` // 2006-01-02, 2006-W02 or 2006-01
	ActivityJSON
	Cells []ActivityCellJSON `json:"cells"`
}

// ActivityCellJSON is the activity of one key in one period.
type ActivityCellJSON struct {
	Key string `json:"key"`
	ActivityJSON
}

// seriesOptions are the resolved --period/--by/--top flags of stats.
type seriesOptions struct {
	period store.Period
	by     store.Dimension
	byName string // "" without --by
	top    int
}

// parseSeriesOptions validates the stats flags that select the period × key
// table. --by without --period defaults to day; --top needs --by.
func parseSeriesOptions(period, by string, top int) (seriesOptions, error) {
	var o seriesOptions
	if period == "" {
		period = store.PeriodDay.String()
	}
	p, err := store.ParsePeriod(period)
	if err != nil {
		return o, err
	}
	o.period = p
	if by != "" {
		d, err := store.ParseDimension(by)
		if err != nil {
			return o, err
		}
		o.by, o.byName = d, by
	}
	switch {
	case top < 0:
		return o, fmt.Errorf("--top must not be negative")
	case top > 0 && by == "":
		return o, fmt.Errorf("--top requires --by")
	}
	o.top = top
	return o, nil
}

// seriesJSON converts a series to its JSON form. names maps raw keys to
// display names; keys without an entry are shown as they are.
func seriesJSON(s store.ActivitySeries, o seriesOptions, names map[string]string, since, until time.Time, tz string) ActivitySeriesJSON {
	out := ActivitySeriesJSON{
		Since: isoTime(since), Until: isoTime(until), Timezone: tz,
		Period: o.period.String(), By: o.byName,
		Keys:    make([]string, 0, len(s.Keys)),
		Buckets: make([]ActivityBucketJSON, 0, len(s.Buckets)),
	}
	name := func(k string) string {
		if n, ok := names[k]; ok {
			return n
		}
		return k
	}
	if o.byName != "" {
		for _, k := range s.Keys {
			out.Keys = append(out.Keys, name(k))
		}
	}
	for _, b := range s.Buckets {
		bj := ActivityBucketJSON{Start: isoTime(b.Start), Label: b.Label, Cells: []ActivityCellJSON{}}
		for _, c := range b.Cells {
			bj.Sessions += c.Sessions
			bj.Human += c.Human
			bj.Agent += c.Agent
			if o.byName != "" && c.Activity != (store.Activity{}) {
				bj.Cells = append(bj.Cells, ActivityCellJSON{Key: name(c.Key), ActivityJSON: act(c.Activity)})
			}
		}
		out.Buckets = append(out.Buckets, bj)
	}
	return out
}

// writeSeriesTable writes the long format: without --by one row per period
// (PERIOD SESSIONS HUMAN AGENT, empty periods as zeros), with --by one row
// per period and key with activity (PERIOD KEY SESSIONS HUMAN AGENT).
func writeSeriesTable(w io.Writer, s ActivitySeriesJSON) error {
	n := func(i int) string { return strconv.Itoa(i) }
	if s.By == "" {
		rows := make([][]string, 0, len(s.Buckets))
		for _, b := range s.Buckets {
			rows = append(rows, []string{b.Label, n(b.Sessions), n(b.Human), n(b.Agent)})
		}
		return table(w, []string{"PERIOD", "SESSIONS", "HUMAN", "AGENT"}, rows)
	}
	var rows [][]string
	for _, b := range s.Buckets {
		for _, c := range b.Cells {
			rows = append(rows, []string{b.Label, c.Key, n(c.Sessions), n(c.Human), n(c.Agent)})
		}
	}
	return table(w, []string{"PERIOD", "KEY", "SESSIONS", "HUMAN", "AGENT"}, rows)
}

// keyNames returns the display names of the series keys: project names for
// the project dimension, else nil (raw keys).
func keyNames(s store.ActivitySeries, o seriesOptions) map[string]string {
	if o.byName == "" || o.by != store.DimProject {
		return nil
	}
	return project.Names(s.Keys)
}
