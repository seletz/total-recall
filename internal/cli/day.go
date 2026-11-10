package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/dayreport"
	"github.com/seletz/total-recall/internal/ops"
	"github.com/seletz/total-recall/internal/store"
)

// DayJSON is the --json output of stats --day: one day chronologically,
// with the numbers of stats --workday --blocks (first, last, span, blocks,
// pauses, target, diff, events) and stats --worktime (time per project).
// The blocks add up to block_seconds, blocks plus pauses to span_seconds,
// the projects of every block to its seconds, human plus agent of the
// blocks to the events of the day, and the project times to
// worktime.raw_seconds.
type DayJSON struct {
	Day           string     `json:"day"`     // 2006-01-02
	Weekday       string     `json:"weekday"` // Mon … Sun
	Timezone      string     `json:"timezone"`
	DayStart      string     `json:"day_start"`
	HumanOnly     bool       `json:"human_only"`
	WorkHours     string     `json:"work_hours"`
	HolidayRegion string     `json:"holiday_region"`
	Weekend       bool       `json:"weekend"`
	Holiday       NullString `json:"holiday"`

	First       NullString `json:"first"`
	Last        NullString `json:"last"`
	FirstLocal  NullString `json:"first_local"`
	LastLocal   NullString `json:"last_local"`
	SpanSeconds int        `json:"span_seconds"`
	// TargetSeconds is the day's target, DiffSeconds the span minus it
	// (zero without a target or activity).
	TargetSeconds   int `json:"target_seconds"`
	DiffSeconds     int `json:"diff_seconds"`
	BlockGapSeconds int `json:"block_gap_seconds"`
	BlockSeconds    int `json:"block_seconds"`
	BreakSeconds    int `json:"break_seconds"`
	ActivityJSON
	Blocks   []DayBlockJSON   `json:"blocks"`
	Worktime DayWorktimeJSON  `json:"worktime"`
	Projects []DayProjectJSON `json:"projects"`
}

// DayBlockJSON is one block of the day.
type DayBlockJSON struct {
	WorkdayBlockJSON
	// PauseSeconds is the gap before the block, zero for the first one.
	PauseSeconds int `json:"pause_seconds"`
	Human        int `json:"human"`
	Agent        int `json:"agent"`
	// Projects are the projects of the block by seconds; the seconds add
	// up to the block's. Sessions are the sessions in it by first event.
	Projects []DayBlockProjectJSON `json:"projects"`
	Sessions []DaySessionJSON      `json:"sessions"`
}

// DayBlockProjectJSON is a project's share of one block.
type DayBlockProjectJSON struct {
	Project string `json:"project"`
	Seconds int    `json:"seconds"`
	Human   int    `json:"human"`
	Agent   int    `json:"agent"`
}

// DaySessionJSON is the part of a session inside a block.
type DaySessionJSON struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Project    string `json:"project"`
	FirstEvent string `json:"first_event"`
	Start      string `json:"start"`
	End        string `json:"end"`
	Human      int    `json:"human"`
	Agent      int    `json:"agent"`
}

// DayWorktimeJSON is the working time of the day as stats --worktime
// estimates it.
type DayWorktimeJSON struct {
	RawSeconds     int `json:"raw_seconds"`
	RoundedSeconds int `json:"rounded_seconds"`
}

// DayProjectJSON is a project of the day with its time (as in --worktime)
// and its counted events.
type DayProjectJSON struct {
	Project        string `json:"project"`
	RawSeconds     int    `json:"raw_seconds"`
	RoundedSeconds int    `json:"rounded_seconds"`
	Human          int    `json:"human"`
	Agent          int    `json:"agent"`
}

// dayReport is a loaded day with what the output needs.
type dayReport struct {
	rep       dayreport.Report
	loc       *time.Location
	tz        string
	dayStart  string
	hours     string
	region    string
	humanOnly bool
	work      store.WorkHours
	blockGap  time.Duration
}

func (r dayReport) json() DayJSON {
	d := r.rep.Workday
	out := DayJSON{
		Day: d.Label, Weekday: d.Day.Format("Mon"), Timezone: r.tz, DayStart: r.dayStart, HumanOnly: r.humanOnly,
		WorkHours: r.hours, HolidayRegion: r.region, Weekend: store.IsWeekend(d.Day), Holiday: NullString(d.Holiday),
		First: NullString(isoTime(d.First)), Last: NullString(isoTime(d.Last)),
		FirstLocal: NullString(d.FirstClock(r.loc)), LastLocal: NullString(d.LastClock(r.loc)),
		SpanSeconds: int(d.Span().Seconds()), TargetSeconds: int(r.rep.Target.Seconds()), DiffSeconds: int(r.rep.Diff.Seconds()),
		BlockGapSeconds: int(r.blockGap.Seconds()), BlockSeconds: d.BlockSeconds(), BreakSeconds: d.BreakSeconds(),
		ActivityJSON: act(d.Activity),
		Blocks:       make([]DayBlockJSON, 0, len(r.rep.Blocks)),
		Worktime:     DayWorktimeJSON{RawSeconds: int(r.rep.Worktime.Raw.Seconds()), RoundedSeconds: int(r.rep.Worktime.Rounded.Seconds())},
		Projects:     make([]DayProjectJSON, 0, len(r.rep.Projects)),
	}
	for _, b := range r.rep.Blocks {
		jb := DayBlockJSON{
			WorkdayBlockJSON: WorkdayBlockJSON{
				Start: isoTime(b.Start), End: isoTime(b.End), StartLocal: d.Clock(b.Start, r.loc), EndLocal: d.Clock(b.End, r.loc),
				Seconds: b.Secs, Events: b.Events, Single: b.Single(),
			},
			PauseSeconds: int(b.Pause.Seconds()), Human: b.Human, Agent: b.Agent,
			Projects: make([]DayBlockProjectJSON, 0, len(b.Projects)), Sessions: make([]DaySessionJSON, 0, len(b.Sessions)),
		}
		for _, p := range b.Projects {
			jb.Projects = append(jb.Projects, DayBlockProjectJSON{Project: p.Name, Seconds: p.Secs, Human: p.Human, Agent: p.Agent})
		}
		for _, s := range b.Sessions {
			jb.Sessions = append(jb.Sessions, DaySessionJSON{ID: s.SessionID, Title: s.Title, Project: s.ProjectName, FirstEvent: s.FirstEvent,
				Start: isoTime(s.Start), End: isoTime(s.End), Human: s.Human, Agent: s.Agent})
		}
		out.Blocks = append(out.Blocks, jb)
	}
	for _, p := range r.rep.Projects {
		out.Projects = append(out.Projects, DayProjectJSON{Project: p.Name, RawSeconds: int(p.Raw.Seconds()), RoundedSeconds: int(p.Rounded.Seconds()), Human: p.Human, Agent: p.Agent})
	}
	return out
}

// table writes the day as text: the header, one line per block with its
// projects and the sessions below, then the projects of the day.
func (r dayReport) table(w io.Writer) error {
	d := r.rep.Workday
	counted := "human and agent events"
	if r.humanOnly {
		counted = "human events only"
	}
	head := d.Day.Format("Mon 2006-01-02")
	if d.IsHoliday() {
		head += " · " + d.Holiday
	} else if store.IsWeekend(d.Day) {
		head += " · weekend"
	}
	_, _ = fmt.Fprintf(w, "%s (%s, day starts %s, %s)\n", head, r.loc, r.dayStart, counted)
	if !d.Active() {
		_, _ = fmt.Fprintln(w, "no activity")
		return nil
	}
	_, _ = fmt.Fprintf(w, "first %s · last %s · span %s · work %s · breaks %s\n", d.FirstClock(r.loc), d.LastClock(r.loc), duration(d.Span()), duration(d.BlockSum()), duration(d.BreakSum()))
	if r.rep.HasDiff {
		_, _ = fmt.Fprintf(w, "target %s · diff %s\n", store.FormatSpan(r.rep.Target), store.FormatDiff(r.rep.Diff))
	}
	_, _ = fmt.Fprintf(w, "events %d human, %d agent · working time %s (rounded %s)\n\n", d.Human, d.Agent, duration(r.rep.Worktime.Raw), duration(r.rep.Worktime.Rounded))
	singles := false
	for i, b := range r.rep.Blocks {
		if b.Pause > 0 {
			_, _ = fmt.Fprintf(w, "  pause %s\n", duration(b.Pause))
		}
		span := d.Clock(b.Start, r.loc) + "-" + d.Clock(b.End, r.loc)
		if b.Single() {
			span, singles = d.Clock(b.Start, r.loc)+"*", true
		}
		var ps []string
		for _, p := range b.Projects {
			ps = append(ps, fmt.Sprintf("%s %s", p.Name, duration(time.Duration(p.Secs)*time.Second)))
		}
		_, _ = fmt.Fprintf(w, "%d. %s  %s  %d events (%d human, %d agent)  %s\n", i+1, span, duration(time.Duration(b.Secs)*time.Second), b.Events, b.Human, b.Agent, strings.Join(ps, ", "))
		for _, s := range b.Sessions {
			_, _ = fmt.Fprintf(w, "     %s-%s  %s  %s  (%d human, %d agent)\n", d.Clock(s.Start, r.loc), d.Clock(s.End, r.loc), s.ProjectName, s.Title, s.Human, s.Agent)
		}
	}
	if singles {
		_, _ = fmt.Fprintln(w, "\n* a block of a single message (zero seconds)")
	}
	rows := make([][]string, 0, len(r.rep.Projects))
	for _, p := range r.rep.Projects {
		rows = append(rows, []string{p.Name, duration(p.Raw), duration(p.Rounded), fmt.Sprint(p.Human), fmt.Sprint(p.Agent)})
	}
	_, _ = fmt.Fprintln(w)
	return table(w, []string{"PROJECT", "TIME", "ROUNDED", "HUMAN", "AGENT"}, rows)
}

// runDay is stats --day: the day chronologically with the numbers of
// --workday --blocks and --worktime.
func (e *env) runDay(cmd *cobra.Command, cfg *config.Config, o *output, dayArg string, humanOnly bool) error {
	loc := cfg.Location()
	dayStart := cfg.DayStartOffset()
	day, err := parseDayArg(dayArg, e.app.Now(), loc, dayStart)
	if err != nil {
		return err
	}
	work, err := ops.WorkHours(cfg)
	if err != nil {
		return err
	}
	settings, err := cfg.WorktimeSettings()
	if err != nil {
		return errKind(ErrKindConfig, err)
	}
	projects, err := cfg.ProjectResolver()
	if err != nil {
		return errKind(ErrKindConfig, err)
	}
	st, err := e.openRead(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	cal := ops.Holidays(cfg)
	rep, err := dayreport.Load(cmd.Context(), st, day, dayreport.Options{
		Location: loc, DayStart: dayStart, BlockGap: cfg.Work.BlockGap.Duration, HumanOnly: humanOnly, Work: work, Holidays: cal,
		Worktime: settings, Projects: projects, Home: cfg.Home,
	})
	if err != nil {
		return err
	}
	r := dayReport{rep: rep, loc: loc, tz: cfg.Timezone, dayStart: cfg.DayStart, hours: cfg.Work.Hours, region: cal.Region(), humanOnly: humanOnly, work: work, blockGap: cfg.Work.BlockGap.Duration}
	if o.format.IsJSON() {
		return o.object(r.json())
	}
	return r.table(o.w)
}

// parseDayArg resolves --day: a date YYYY-MM-DD, today or yesterday (under
// the day rule, so shortly after midnight today is still the previous
// calendar day).
func parseDayArg(s string, now time.Time, loc *time.Location, dayStart time.Duration) (time.Time, error) {
	switch s {
	case "today":
		return store.DayOf(now, loc, dayStart), nil
	case "yesterday":
		return store.DayOf(now, loc, dayStart).AddDate(0, 0, -1), nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return t, usagef("invalid --day %q (use YYYY-MM-DD, today or yesterday)", s)
	}
	return t, nil
}
