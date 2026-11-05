package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/worktime"
)

// WorktimeJSON is the --json output of stats --worktime: the estimated
// working time per day and category, raw and rounded, with the settings
// it was computed under.
type WorktimeJSON struct {
	Since    NullString `json:"since"`
	Until    NullString `json:"until"`
	Timezone string     `json:"timezone"`
	DayStart string     `json:"day_start"` // HH:MM, activity before it counts for the previous day
	// MaxGapSeconds is the longest gap between two own messages that
	// counts in full, MinCreditSeconds what a longer gap counts.
	MaxGapSeconds    int `json:"max_gap_seconds"`
	MinCreditSeconds int `json:"min_credit_seconds"`
	// GridSeconds, Rounding (up, down, nearest) and RoundingScope
	// (category, project) describe the rounding.
	GridSeconds   int    `json:"grid_seconds"`
	Rounding      string `json:"rounding"`
	RoundingScope string `json:"rounding_scope"`
	// SummaryMaxChars is the length the booking text is cut to, 0 for no
	// limit.
	SummaryMaxChars int `json:"summary_max_chars"`
	// HolidayRegion is the Bundesland whose public holidays are named.
	HolidayRegion string `json:"holiday_region"`
	// Days are all days of the range in order; a day without time has an
	// empty category list and zeros.
	Days []WorktimeDayJSON `json:"days"`
	// Categories sums every category over the range, sorted by name.
	Categories []WorktimeCategoryTotalJSON `json:"categories"`
	Total      WorktimeTotalJSON           `json:"total"`
}

// WorktimeDayJSON is one day.
type WorktimeDayJSON struct {
	Day     string     `json:"day"`     // 2006-01-02 in local time
	Weekday string     `json:"weekday"` // Mon … Sun
	Weekend bool       `json:"weekend"`
	Holiday NullString `json:"holiday"` // name of the public holiday, null on an ordinary day
	// RawSeconds and RoundedSeconds sum the categories of the day.
	RawSeconds     int                `json:"raw_seconds"`
	RoundedSeconds int                `json:"rounded_seconds"`
	Categories     []WorktimeCellJSON `json:"categories"`
}

// WorktimeCellJSON is one category on one day. Refs are the issue and
// pull-request references of the titles and own messages of its sessions
// (#31, PR #32, owner/repo#123), deduplicated and sorted, Text the
// booking text "<project>: <title> (<refs>), …; <project>: …" cut to
// summary_max_chars.
type WorktimeCellJSON struct {
	Category string `json:"category"`
	// RawSeconds is the credited time, RoundedSeconds the time to book.
	RawSeconds     int                   `json:"raw_seconds"`
	RoundedSeconds int                   `json:"rounded_seconds"`
	Projects       []WorktimeProjectJSON `json:"projects"`
	Sessions       []WorktimeSessionJSON `json:"sessions"`
	Refs           []string              `json:"refs"`
	Text           string                `json:"text"`
}

// WorktimeProjectJSON is one project within a category on one day; Remote
// is null without a repository. RoundedSeconds rounds the project alone.
// Refs are the references of the project's sessions.
type WorktimeProjectJSON struct {
	Project        string     `json:"project"`
	Key            string     `json:"key"` // git root, else working directory
	Remote         NullString `json:"remote"`
	RawSeconds     int        `json:"raw_seconds"`
	RoundedSeconds int        `json:"rounded_seconds"`
	Refs           []string   `json:"refs"`
}

// WorktimeSessionJSON is one session within a category on one day, in
// order of its first message; Title is null without a title. Refs are the
// references of the title and the own messages of the day.
type WorktimeSessionJSON struct {
	ID         string     `json:"id"`
	Title      NullString `json:"title"`
	RawSeconds int        `json:"raw_seconds"`
	Refs       []string   `json:"refs"`
}

// WorktimeCategoryTotalJSON sums one category over the range.
type WorktimeCategoryTotalJSON struct {
	Category string `json:"category"`
	WorktimeTotalJSON
}

// WorktimeTotalJSON sums the range: RoundedSeconds is the sum of the
// rounded day cells, Days counts the days with time.
type WorktimeTotalJSON struct {
	RawSeconds     int `json:"raw_seconds"`
	RoundedSeconds int `json:"rounded_seconds"`
	Days           int `json:"days"`
}

// worktimeReport is the output of stats --worktime with its settings.
type worktimeReport struct {
	rep          worktime.Report
	since, until time.Time
	loc          *time.Location
	tz, dayStart string
	settings     worktime.Settings
	region       string
	sessions     bool // text output lists sessions and projects per cell
	summary      bool // text output adds refs and booking text per cell
}

// runWorktime is stats --worktime: the working time per day and category
// estimated from the gaps between the user's own messages.
func (e *env) runWorktime(cmd *cobra.Command, cfg *config.Config, o *output, rng, month, sinceArg, untilArg string, sessions, summary bool) error {
	loc := cfg.Location()
	settings, err := cfg.WorktimeSettings()
	if err != nil {
		return errKind(ErrKindConfig, err)
	}
	projects, err := cfg.ProjectResolver()
	if err != nil {
		return errKind(ErrKindConfig, err)
	}
	since, until, err := workdayRange(rng, month, sinceArg, untilArg, e.app.Now(), loc, cfg.DayStartOffset())
	if err != nil {
		return err
	}
	st, err := e.openRead(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	events, err := st.HumanEvents(cmd.Context(), store.HumanEventsFilter{Since: since, Until: until})
	if err != nil {
		return err
	}
	in := make([]worktime.Event, len(events))
	for i, ev := range events {
		in[i] = worktime.Event{Time: ev.Time, Session: ev.SessionID, Title: ev.Title, Project: ev.Project, Dir: ev.Dir, Remote: ev.RemoteURL, Text: ev.Text}
	}
	cal := holidays(cfg)
	opts := worktime.Options{Location: loc, DayStart: cfg.DayStartOffset(), Home: cfg.Home, Holidays: cal, Projects: projects}
	if !since.IsZero() {
		opts.From = store.DayOf(since, loc, cfg.DayStartOffset())
	}
	if !until.IsZero() {
		opts.To = store.DayOf(until.Add(-time.Nanosecond), loc, cfg.DayStartOffset())
	}
	r := worktimeReport{
		rep: worktime.Compute(in, settings, opts), since: since, until: until, loc: loc, tz: cfg.Timezone, dayStart: cfg.DayStart,
		settings: settings, region: cal.Region(), sessions: sessions, summary: summary,
	}
	if o.format.IsJSON() {
		return o.object(r.json())
	}
	return r.text(o.w)
}

// seconds converts a duration to whole seconds.
func seconds(d time.Duration) int { return int(d.Seconds()) }

// refStrings writes the references for JSON; never nil.
func refStrings(refs []worktime.Ref) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.String())
	}
	return out
}

// json converts the report to its JSON form.
func (r worktimeReport) json() WorktimeJSON {
	s := r.settings
	out := WorktimeJSON{
		Since: NullString(isoTime(r.since)), Until: NullString(isoTime(r.until)), Timezone: r.tz, DayStart: r.dayStart,
		MaxGapSeconds: seconds(s.MaxGap), MinCreditSeconds: seconds(s.MinCredit), GridSeconds: seconds(s.Grid),
		Rounding: s.Rounding.String(), RoundingScope: s.Scope.String(), SummaryMaxChars: s.SummaryMaxChars, HolidayRegion: r.region,
		Days: make([]WorktimeDayJSON, 0, len(r.rep.Days)), Categories: make([]WorktimeCategoryTotalJSON, 0, len(r.rep.Categories)),
	}
	for _, d := range r.rep.Days {
		dj := WorktimeDayJSON{
			Day: d.Label, Weekday: d.Day.Format("Mon"), Weekend: store.IsWeekend(d.Day), Holiday: NullString(d.Holiday),
			RawSeconds: seconds(d.Raw), RoundedSeconds: seconds(d.Rounded), Categories: make([]WorktimeCellJSON, 0, len(d.Categories)),
		}
		for _, c := range d.Categories {
			cj := WorktimeCellJSON{
				Category: c.Name, RawSeconds: seconds(c.Raw), RoundedSeconds: seconds(c.Rounded),
				Projects: make([]WorktimeProjectJSON, 0, len(c.Projects)), Sessions: make([]WorktimeSessionJSON, 0, len(c.Sessions)),
				Refs: refStrings(c.Refs), Text: c.Text,
			}
			for _, p := range c.Projects {
				cj.Projects = append(cj.Projects, WorktimeProjectJSON{Project: p.Name, Key: p.Key, Remote: NullString(p.Remote), RawSeconds: seconds(p.Raw), RoundedSeconds: seconds(p.Rounded), Refs: refStrings(p.Refs)})
			}
			for _, sess := range c.Sessions {
				cj.Sessions = append(cj.Sessions, WorktimeSessionJSON{ID: sess.ID, Title: NullString(sess.Title), RawSeconds: seconds(sess.Raw), Refs: refStrings(sess.Refs)})
			}
			dj.Categories = append(dj.Categories, cj)
		}
		out.Days = append(out.Days, dj)
	}
	for _, c := range r.rep.Categories {
		out.Categories = append(out.Categories, WorktimeCategoryTotalJSON{Category: c.Name, WorktimeTotalJSON: totalJSON(c)})
	}
	out.Total = totalJSON(r.rep.Total)
	return out
}

func totalJSON(t worktime.Total) WorktimeTotalJSON {
	return WorktimeTotalJSON{RawSeconds: seconds(t.Raw), RoundedSeconds: seconds(t.Rounded), Days: t.Days}
}

// text writes the range and the settings, one row per day and category
// (DATE DAY CATEGORY RAW ROUNDED HOLIDAY, a day without time as one row
// with "-"), with --sessions or --summary one block per day instead (see
// details), and the totals per category.
func (r worktimeReport) text(w io.Writer) error {
	rng := "all time"
	if !r.since.IsZero() || !r.until.IsZero() {
		end := ""
		if !r.until.IsZero() {
			end = localTime(r.until.Add(-time.Minute), r.loc)
		}
		rng = localTime(r.since, r.loc) + " – " + end
	}
	s := r.settings
	_, _ = fmt.Fprintf(w, "%s (%s, day starts %s, human events only)\n", rng, r.loc, r.dayStart)
	rounding := "no rounding"
	if s.Grid > 0 {
		rounding = fmt.Sprintf("rounded %s %s per %s and day", roundingWord(s.Rounding), store.FormatSpan(s.Grid), s.Scope)
	}
	_, _ = fmt.Fprintf(w, "gaps up to %s count in full, longer ones %s · %s · holidays %s\n", store.FormatSpan(s.MaxGap), store.FormatSpan(s.MinCredit), rounding, r.region)
	_, _ = fmt.Fprintln(w, "a lower bound: meetings and work without an agent leave no messages")
	_, _ = fmt.Fprintln(w)
	details := r.sessions || r.summary
	if details {
		r.details(w)
	} else if err := r.table(w); err != nil {
		return err
	}
	if details && r.rep.Total.Days == 0 {
		_, _ = fmt.Fprintln(w, "no activity")
	}
	_, _ = fmt.Fprintln(w)
	rows := make([][]string, 0, len(r.rep.Categories)+1)
	for _, c := range r.rep.Categories {
		rows = append(rows, []string{c.Name, span(c.Raw), span(c.Rounded), strconv.Itoa(c.Days)})
	}
	t := r.rep.Total
	rows = append(rows, []string{"total", span(t.Raw), span(t.Rounded), strconv.Itoa(t.Days)})
	return table(w, []string{"CATEGORY", "RAW", "ROUNDED", "DAYS"}, rows)
}

// table writes the day rows.
func (r worktimeReport) table(w io.Writer) error {
	var rows [][]string
	for _, d := range r.rep.Days {
		holiday := "-"
		if d.Holiday != "" {
			holiday = d.Holiday
		}
		if !d.Active() {
			rows = append(rows, []string{d.Label, d.Day.Format("Mon"), "-", "-", "-", holiday})
			continue
		}
		for _, c := range d.Categories {
			rows = append(rows, []string{d.Label, d.Day.Format("Mon"), c.Name, span(c.Raw), span(c.Rounded), holiday})
		}
	}
	return table(w, []string{"DATE", "DAY", "CATEGORY", "RAW", "ROUNDED", "HOLIDAY"}, rows)
}

// details writes one block per day with time (inactive days are left
// out): the categories with their time, and below each, with --sessions,
// the projects (when the category has more than one) and the sessions
// with their titles, and with --summary the references per project and
// the booking text.
func (r worktimeReport) details(w io.Writer) {
	n := 0
	for _, d := range r.rep.Days {
		if !d.Active() {
			continue
		}
		if n > 0 {
			_, _ = fmt.Fprintln(w)
		}
		n++
		head := d.Label + " " + d.Day.Format("Mon")
		if d.Holiday != "" {
			head += " · " + d.Holiday
		}
		_, _ = fmt.Fprintf(w, "%s: %s raw, %s rounded\n", head, span(d.Raw), span(d.Rounded))
		for _, c := range d.Categories {
			_, _ = fmt.Fprintf(w, "  %s: %s raw, %s rounded\n", c.Name, span(c.Raw), span(c.Rounded))
			if !r.sessions {
				r.summaryLines(w, c)
				continue
			}
			if len(c.Projects) > 1 {
				names := make([]string, len(c.Projects))
				for i, p := range c.Projects {
					names[i] = fmt.Sprintf("%s %s", p.Name, span(p.Raw))
				}
				_, _ = fmt.Fprintf(w, "    projects: %s\n", strings.Join(names, ", "))
			}
			for _, sess := range c.Sessions {
				title := sess.Title
				if title == "" {
					title = "(untitled)"
				}
				_, _ = fmt.Fprintf(w, "    %6s  %s  %s\n", span(sess.Raw), worktimeShortID(sess.ID), title)
			}
			r.summaryLines(w, c)
		}
	}
}

// summaryLines writes the references of a cell grouped by project
// ("refs: kompass #21, #31; wetterwarte #238", "-" without any) and the
// booking text, with --summary.
func (r worktimeReport) summaryLines(w io.Writer, c worktime.Category) {
	if !r.summary {
		return
	}
	var groups []string
	for _, p := range c.Projects {
		if len(p.Refs) > 0 {
			groups = append(groups, p.Name+" "+strings.Join(refStrings(p.Refs), ", "))
		}
	}
	refs := "-"
	if len(groups) > 0 {
		refs = strings.Join(groups, "; ")
	}
	_, _ = fmt.Fprintf(w, "    refs: %s\n", refs)
	_, _ = fmt.Fprintf(w, "    text: %s\n", c.Text)
}

// worktimeShortID shortens a session id like the sessions table: the
// first eight characters of the native id (fourteen for a subagent).
func worktimeShortID(id string) string {
	_, native, ok := strings.Cut(id, ":")
	if !ok {
		return id
	}
	n := 8
	if strings.HasPrefix(native, "agent-") {
		n = 14
	}
	if len(native) <= n {
		return native
	}
	return native[:n]
}

// span formats a duration for the tables; "0m" for zero.
func span(d time.Duration) string {
	if d == 0 {
		return "0m"
	}
	return store.FormatSpan(d)
}

// roundingWord names the direction in the header, before the grid:
// "up to", "down to", "to the nearest".
func roundingWord(r worktime.Rounding) string {
	if r == worktime.Nearest {
		return "to the nearest"
	}
	return r.String() + " to"
}
