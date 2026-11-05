package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/canvas"
	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/figure"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/worktime"
)

// Chart names of `total-recall chart`, in the order `all` draws them.
var chartNames = []string{"calendar", "projects", "punchcard", "worktime"}

// OtherColor is the colour of the folded projects (store.OtherKey) in the
// project chart.
const OtherColor = "#8c8c8c"

// chartFlags are the flags of chart after the config defaults were applied.
type chartFlags struct {
	month, since, until, rng string
	format                   string
	width, height            int
	scale                    float64
	out                      string
	noTitle, noLegend        bool
	transparent              bool
	top                      int
}

func (e *env) chartCmd() *cobra.Command {
	var f chartFlags
	c := &cobra.Command{
		Use:   "chart <" + strings.Join(chartNames, "|") + "|all>",
		Short: "Draw the figures of a monthly report as SVG or PNG",
		Long: `Draws one figure or all four of them for a time range, as SVG (default) or
PNG, into files below --out or, for a single figure, to stdout:

  calendar   own messages per day as a calendar heatmap (weeks as columns)
  projects   share of agent work and own attention per project, two bars each
  punchcard  own messages and agent activity per weekday × hour
  worktime   working time per week and category, stacked, with the target

The range is the current month, --month YYYY-MM, --since/--until or --range
today|this-week|last-week|this-month|last-month. The counts are those of
stats: own messages are events of actor human, so prompts of other agents
(peer) and automation do not count. worktime uses the rules of
stats --worktime and [worktime]; its target per week is the daily target of
[work] times the workdays of the week (weekdays that are no public holiday,
up to today).

Files are named <YYYY-MM>-<figure>.<format> for a month, else
<since>_<until>-<figure>.<format>. --scale multiplies the natural size,
--width or --height set it instead (both: the figure is centred in the box).
Colours: projects take the color of their [[project]] definition, else the
palette in order of volume; categories the color of [[worktime.category]],
else the palette in order of the rules; the heatmap [chart] heatmap.
[chart] holds the defaults of the format and switches.`,
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return e.runChart(cmd, args[0], f)
		},
	}
	c.Flags().StringVar(&f.month, "month", "", "month YYYY-MM (default: current month)")
	c.Flags().StringVar(&f.since, "since", "", "from this time (instead of --month)")
	c.Flags().StringVar(&f.until, "until", "", "until this time; a date includes the whole day")
	c.Flags().StringVar(&f.rng, "range", "", strings.Join(rangeNames, ", ")+" (instead of --month/--since/--until)")
	c.Flags().StringVar(&f.format, "format", "", "svg or png (default from [chart], svg)")
	c.Flags().IntVar(&f.width, "width", 0, "width in pixels (default: natural size × --scale)")
	c.Flags().IntVar(&f.height, "height", 0, "height in pixels")
	c.Flags().Float64Var(&f.scale, "scale", 0, "size factor (default from [chart], 1)")
	c.Flags().StringVar(&f.out, "out", "", "directory for the files (required for all; default: a single figure to stdout)")
	c.Flags().BoolVar(&f.noTitle, "no-title", false, "leave the heading out")
	c.Flags().BoolVar(&f.noLegend, "no-legend", false, "leave the legend out")
	c.Flags().BoolVar(&f.transparent, "transparent", false, "transparent instead of white background")
	c.Flags().IntVar(&f.top, "top", -1, "projects: keep the N largest, fold the rest into \"other\" (0 = all; default from [chart], 12)")
	return c
}

// chartRun is one run of chart: the settings and the lazily loaded data
// the figures share.
type chartRun struct {
	cfg      *config.Config
	st       *store.Store
	settings config.ChartSettings
	opts     figure.Options
	size     canvas.Size
	format   figure.Format
	top      int
	now      time.Time
	// since/until bound the events (midnight bounds); daySince/dayUntil
	// are the same range under the day rule for the working time.
	since, until       time.Time
	daySince, dayUntil time.Time
	stats              *store.Stats
}

// runChart resolves the flags and draws the requested figures.
func (e *env) runChart(cmd *cobra.Command, which string, f chartFlags) error {
	if which != "all" && !slices.Contains(chartNames, which) {
		return usagef("unknown figure %q (use %s or all)", which, strings.Join(chartNames, ", "))
	}
	if which == "all" && f.out == "" {
		return usagef("all needs --out DIR")
	}
	if f.width < 0 || f.height < 0 {
		return usagef("--width and --height must not be negative")
	}
	if cmd.Flags().Changed("scale") && f.scale <= 0 {
		return usagef("--scale must be positive")
	}
	if f.top < -1 {
		return usagef("--top must not be negative")
	}
	cfg, err := e.loadConfig()
	if err != nil {
		return err
	}
	settings, err := cfg.ChartSettings()
	if err != nil {
		return errKind(ErrKindConfig, err)
	}
	r := &chartRun{cfg: cfg, settings: settings, now: e.app.Now()}
	formatName := settings.Format
	if f.format != "" {
		formatName = f.format
	}
	if r.format, err = figure.ParseFormat(formatName); err != nil {
		return usage(err)
	}
	r.size = canvas.Size{Width: f.width, Height: f.height, Scale: settings.Scale}
	if f.scale > 0 {
		r.size.Scale = f.scale
	}
	r.top = settings.Top
	if f.top >= 0 {
		r.top = f.top
	}
	loc := cfg.Location()
	if r.since, r.until, err = chartRange(f, r.now, loc, 0); err != nil {
		return err
	}
	if r.daySince, r.dayUntil, err = chartRange(f, r.now, loc, cfg.DayStartOffset()); err != nil {
		return err
	}
	r.opts = figure.Options{
		Period:      figure.PeriodName(r.since, r.until, loc),
		NoTitle:     !settings.Title || f.noTitle,
		NoLegend:    !settings.Legend || f.noLegend,
		Transparent: settings.Transparent || f.transparent,
	}
	if r.st, err = e.openRead(cfg); err != nil {
		return err
	}
	defer func() { _ = r.st.Close() }()

	names := chartNames
	if which != "all" {
		names = []string{which}
	}
	slug := figure.PeriodSlug(r.since, r.until, loc)
	for _, name := range names {
		scene, err := r.draw(cmd, name)
		if err != nil {
			return err
		}
		data, err := figure.Encode(scene, r.format, r.size)
		if err != nil {
			return err
		}
		if f.out == "" {
			_, err := e.app.Stdout.Write(data)
			return err
		}
		if err := os.MkdirAll(f.out, 0o755); err != nil {
			return err
		}
		path := filepath.Join(f.out, fmt.Sprintf("%s-%s.%s", slug, name, r.format))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(e.app.Stdout, path)
	}
	return nil
}

// chartRange resolves the range of chart: --range, else --month or
// --since/--until, else the current month. dayStart shifts the bounds of
// a whole-day range to the day boundary (the working time), 0 keeps
// midnight (the event counts, as stats does).
func chartRange(f chartFlags, now time.Time, loc *time.Location, dayStart time.Duration) (time.Time, time.Time, error) {
	explicit := f.month != "" || f.since != "" || f.until != ""
	switch {
	case f.rng != "" && explicit:
		return time.Time{}, time.Time{}, usagef("use either --range or --month/--since/--until")
	case f.rng != "":
		return rangeBounds(f.rng, now, loc, dayStart)
	case explicit:
		return workdayBounds(f.month, f.since, f.until, loc, dayStart)
	}
	since, until, err := statsPeriod("", "", "", now.In(loc), loc)
	if err != nil {
		return since, until, err
	}
	return store.DayBoundary(since, dayStart), store.DayBoundary(until, dayStart), nil
}

// draw builds the scene of one figure.
func (r *chartRun) draw(cmd *cobra.Command, name string) (canvas.Scene, error) {
	switch name {
	case "calendar":
		return r.calendar(cmd)
	case "projects":
		return r.projects(cmd)
	case "punchcard":
		return r.punchcard(cmd)
	case "worktime":
		return r.worktime(cmd)
	}
	return canvas.Scene{}, fmt.Errorf("chart: unknown figure %q", name)
}

// loadStats runs store.Stats once for the calendar and the punchcard.
func (r *chartRun) loadStats(cmd *cobra.Command) (store.Stats, error) {
	if r.stats == nil {
		s, err := r.st.Stats(cmd.Context(), store.StatsFilter{Since: r.since, Until: r.until, Location: r.cfg.Location()})
		if err != nil {
			return s, err
		}
		r.stats = &s
	}
	return *r.stats, nil
}

func (r *chartRun) calendar(cmd *cobra.Command) (canvas.Scene, error) {
	s, err := r.loadStats(cmd)
	if err != nil {
		return canvas.Scene{}, err
	}
	loc := r.cfg.Location()
	d := figure.CalendarData{Values: make(map[string]int, len(s.ByDay))}
	for _, day := range s.ByDay {
		d.Values[day.Day] = day.Human
	}
	// Open bounds take the first and the last day with activity.
	if !r.since.IsZero() {
		d.First = midnight(r.since, loc)
	} else if len(s.ByDay) > 0 {
		d.First, _ = time.ParseInLocation("2006-01-02", s.ByDay[0].Day, loc)
	}
	if !r.until.IsZero() {
		d.Last = midnight(r.until.Add(-time.Second), loc)
	} else if len(s.ByDay) > 0 {
		d.Last, _ = time.ParseInLocation("2006-01-02", s.ByDay[len(s.ByDay)-1].Day, loc)
	}
	if d.First.IsZero() {
		d.First = midnight(r.now, loc)
	}
	if d.Last.IsZero() {
		d.Last = d.First
	}
	scale, err := figure.Colors(r.settings.Heatmap)
	if err != nil {
		return canvas.Scene{}, errKind(ErrKindConfig, err)
	}
	return figure.Calendar(d, scale, r.opts), nil
}

func (r *chartRun) projects(cmd *cobra.Command) (canvas.Scene, error) {
	projects, err := r.cfg.ProjectResolver()
	if err != nil {
		return canvas.Scene{}, errKind(ErrKindConfig, err)
	}
	as, err := r.st.ActivitySeries(cmd.Context(), store.ActivityFilter{
		Since: r.since, Until: r.until, Location: r.cfg.Location(),
		Period: store.PeriodMonth, Dimension: store.DimProject, TopN: r.top, Project: projects.Key,
	})
	if err != nil {
		return canvas.Scene{}, err
	}
	names := projects.Names(as.Keys, as.Dirs, r.cfg.Home)
	rows := make([]figure.ProjectRow, len(as.Keys))
	for i, k := range as.Keys {
		rows[i].Name = k
		if n, ok := names[k]; ok {
			rows[i].Name = n
		}
	}
	for _, b := range as.Buckets {
		for i, c := range b.Cells {
			rows[i].Human += c.Human
			rows[i].Agent += c.Agent
		}
	}
	// Configured colours ([[project]] color) are fixed, the rest take the
	// palette in order of volume.
	keys := make([]string, 0, len(rows))
	fixed := map[string]string{}
	for _, row := range rows {
		keys = append(keys, row.Name)
		if c := projects.Color(row.Name); c != "" {
			fixed[row.Name] = c
		}
	}
	colors := figure.AssignColors(keys, fixed)
	for i := range rows {
		rows[i].Color = colors[rows[i].Name]
		if as.Keys[i] == store.OtherKey {
			rows[i].Color = canvas.Hex(OtherColor)
		}
	}
	return figure.Projects(rows, r.opts), nil
}

func (r *chartRun) punchcard(cmd *cobra.Command) (canvas.Scene, error) {
	s, err := r.loadStats(cmd)
	if err != nil {
		return canvas.Scene{}, err
	}
	var d figure.PunchcardData
	for wd := range 7 {
		for h := range 24 {
			d.Human[wd][h] = s.ByWeekdayHour[wd][h].Human
			d.Agent[wd][h] = s.ByWeekdayHour[wd][h].Agent
		}
	}
	return figure.Punchcard(d, r.opts), nil
}

func (r *chartRun) worktime(cmd *cobra.Command) (canvas.Scene, error) {
	cfg := r.cfg
	settings, err := cfg.WorktimeSettings()
	if err != nil {
		return canvas.Scene{}, errKind(ErrKindConfig, err)
	}
	projects, err := cfg.ProjectResolver()
	if err != nil {
		return canvas.Scene{}, errKind(ErrKindConfig, err)
	}
	loc := cfg.Location()
	events, err := r.st.HumanEvents(cmd.Context(), store.HumanEventsFilter{Since: r.daySince, Until: r.dayUntil})
	if err != nil {
		return canvas.Scene{}, err
	}
	in := make([]worktime.Event, len(events))
	for i, ev := range events {
		in[i] = worktime.Event{Time: ev.Time, Session: ev.SessionID, Title: ev.Title, Project: ev.Project, Dir: ev.Dir, Remote: ev.RemoteURL, Text: ev.Text}
	}
	opts := worktime.Options{Location: loc, DayStart: cfg.DayStartOffset(), Home: cfg.Home, Holidays: holidays(cfg), Projects: projects}
	if !r.daySince.IsZero() {
		opts.From = store.DayOf(r.daySince, loc, cfg.DayStartOffset())
	}
	if !r.dayUntil.IsZero() {
		opts.To = store.DayOf(r.dayUntil.Add(-time.Nanosecond), loc, cfg.DayStartOffset())
	}
	rep := worktime.Compute(in, settings, opts)
	asOf := store.DayOf(r.now, loc, cfg.DayStartOffset())
	target := cfg.Work.Target.Duration

	// Categories in the order of the rules, then the rest by name.
	seen := map[string]bool{}
	var order []string
	for _, c := range rep.Categories {
		seen[c.Name] = true
	}
	for _, rule := range settings.Rules {
		if seen[rule.Name] {
			order = append(order, rule.Name)
			seen[rule.Name] = false
		}
	}
	var rest []string
	for n, ok := range seen {
		if ok {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	order = append(order, rest...)
	colors := figure.AssignColors(order, r.settings.CategoryColors)
	d := figure.WorktimeData{}
	for _, n := range order {
		d.Categories = append(d.Categories, figure.WorktimeCategory{Name: n, Color: colors[n]})
	}
	weeks := map[string]*figure.WorktimeWeek{}
	var labels []string
	for _, day := range rep.Days {
		y, w := day.Day.ISOWeek()
		key := fmt.Sprintf("%d-W%02d", y, w)
		wk := weeks[key]
		if wk == nil {
			wk = &figure.WorktimeWeek{Label: fmt.Sprintf("W%02d", w), Hours: map[string]float64{}}
			weeks[key] = wk
			labels = append(labels, key)
		}
		for _, c := range day.Categories {
			wk.Hours[c.Name] += c.Rounded.Hours()
		}
		if !store.IsWeekend(day.Day) && day.Holiday == "" && !day.Day.After(asOf) {
			wk.Target += target.Hours()
		}
	}
	for _, key := range labels {
		d.Weeks = append(d.Weeks, *weeks[key])
	}
	return figure.Worktime(d, r.opts), nil
}

// midnight returns the local midnight of the calendar day of t.
func midnight(t time.Time, loc *time.Location) time.Time {
	lt := t.In(loc)
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc)
}
