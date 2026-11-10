// Package dayreport assembles one day of the archive for the day views:
// the header of stats --workday --blocks, the activity blocks of the day
// with their projects and sessions, and the projects of the day with the
// time of stats --worktime and the events split by human and agent. It
// calculates nothing itself that the store and worktime packages already
// calculate: the span, blocks and pauses are store.Workday, the project
// times are worktime.Compute, the target and balance are store.WorkHours.
// What it adds is the attribution of events to blocks, projects and
// sessions, and the seconds of a block by project.
//
// Load fails when the pieces do not add up (the archive changed between
// two of its queries); callers load again.
package dayreport

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/worktime"
)

// Source is the part of the store Load reads; *store.Store implements it.
type Source interface {
	Workday(ctx context.Context, f store.WorkdayFilter) ([]store.Workday, error)
	HumanEvents(ctx context.Context, f store.HumanEventsFilter) ([]store.HumanEvent, error)
	Timeline(ctx context.Context, f store.TimelineFilter) ([]store.TimelineEvent, error)
}

// Options are the rules the numbers are counted under: the same as of
// stats --workday and --worktime.
type Options struct {
	Location *time.Location
	// DayStart is the day boundary as an offset from midnight.
	DayStart time.Duration
	// BlockGap ends a block ([work] block_gap); it must be above zero.
	BlockGap time.Duration
	// HumanOnly counts own messages only, like stats --workday.
	HumanOnly bool
	Work      store.WorkHours
	Holidays  store.Holidays
	// Worktime are the settings of the estimate ([worktime]).
	Worktime worktime.Settings
	// Projects renames project keys ([[project]]); Home abbreviates paths.
	Projects project.Resolver
	Home     string
}

// Report is one day.
type Report struct {
	// Workday is the day as stats --workday --blocks gives it: first and
	// last activity, span, blocks, counted events.
	Workday store.Workday
	// Target is the daily target of the day (zero on a day without one);
	// Diff the span minus the target, valid with HasDiff; Rating its
	// rating against the bounds.
	Target  time.Duration
	Diff    time.Duration
	HasDiff bool
	Rating  store.Rating
	// Worktime is the day of stats --worktime (raw and rounded sum).
	Worktime worktime.Day
	// Blocks are the activity blocks in order.
	Blocks []Block
	// Projects are the projects of the day, by time descending, then by
	// events, then by name.
	Projects []Project
}

// Block is a store.Block with what happened in it.
type Block struct {
	store.Block
	// Pause is the gap before the block, zero for the first one.
	Pause    time.Duration
	Human    int
	Agent    int
	Projects []BlockProject // by seconds descending, then by key
	Sessions []Segment      // by first event
}

// BlockProject is a project's share of a block: Secs sum to the block's
// Secs (each interval between two events counts for the project of the
// later one, the first event of the block for none), Human and Agent are
// its events.
type BlockProject struct {
	Key, Name    string
	Secs         int
	Human, Agent int
}

// Segment is the part of a session inside a block.
type Segment struct {
	SessionID, Title string
	Project          string // key
	ProjectName      string
	FirstEvent       string // ID of the first counted event in the block
	Start, End       time.Time
	Human, Agent     int
}

// Project is a project of the day.
type Project struct {
	Key, Name string
	Dir       bool
	// Raw and Rounded are the time of stats --worktime.
	Raw, Rounded time.Duration
	// Human and Agent are the counted events.
	Human, Agent int
}

// Load returns the day with the calendar date of day in o.Location (any
// time of it; the day runs from DayStart on that date to DayStart on the
// next).
func Load(ctx context.Context, src Source, day time.Time, o Options) (Report, error) {
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.BlockGap <= 0 {
		return Report{}, fmt.Errorf("dayreport: block gap must be above zero")
	}
	y, m, d := day.In(o.Location).Date()
	day = time.Date(y, m, d, 0, 0, 0, 0, o.Location)
	since, until := store.DayBoundary(day, o.DayStart), store.DayBoundary(day.AddDate(0, 0, 1), o.DayStart)

	days, err := src.Workday(ctx, store.WorkdayFilter{Since: since, Until: until, Location: o.Location, DayStart: o.DayStart, HumanOnly: o.HumanOnly, Holidays: o.Holidays, BlockGap: o.BlockGap})
	if err != nil {
		return Report{}, err
	}
	wd := store.Workday{Day: day, Label: day.Format("2006-01-02")}
	if o.Holidays != nil {
		wd.Holiday = o.Holidays.Name(day)
	}
	for _, d := range days {
		if d.Day.Equal(day) {
			wd = d
		}
	}
	r := Report{Workday: wd}
	r.Target = o.Work.TargetFor(wd)
	r.Diff, r.HasDiff = o.Work.Diff(wd)
	r.Rating = o.Work.Rate(wd)

	wt, err := worktimeDay(ctx, src, o, day, since, until)
	if err != nil {
		return Report{}, err
	}
	r.Worktime = wt
	if !wd.Active() {
		r.Projects = projectsOf(nil, wt, o)
		return r, nil
	}

	events, err := timeline(ctx, src, o, since, until)
	if err != nil {
		return Report{}, err
	}
	if len(events) != wd.Human+wd.Agent {
		return Report{}, fmt.Errorf("dayreport: %s: %d events, stats count %d (the archive changed while loading)", wd.Label, len(events), wd.Human+wd.Agent)
	}
	if err := r.attribute(events, o); err != nil {
		return Report{}, err
	}
	r.Projects = projectsOf(events, wt, o)
	return r, nil
}

func worktimeDay(ctx context.Context, src Source, o Options, day, since, until time.Time) (worktime.Day, error) {
	evs, err := src.HumanEvents(ctx, store.HumanEventsFilter{Since: since, Until: until})
	if err != nil {
		return worktime.Day{}, err
	}
	in := make([]worktime.Event, len(evs))
	for i, e := range evs {
		in[i] = worktime.Event{Time: e.Time, Session: e.SessionID, Title: e.Title, Project: e.Project, Dir: e.Dir, Remote: e.RemoteURL, Text: e.Text}
	}
	rep := worktime.Compute(in, o.Worktime, worktime.Options{Location: o.Location, DayStart: o.DayStart, Home: o.Home, Projects: o.Projects, From: day, To: day, Holidays: o.Holidays})
	if len(rep.Days) == 0 {
		return worktime.Day{Day: day, Label: day.Format("2006-01-02")}, nil
	}
	return rep.Days[0], nil
}

// timeline returns the counted events of the range in time order.
func timeline(ctx context.Context, src Source, o Options, since, until time.Time) ([]store.TimelineEvent, error) {
	actors := []model.Actor{model.ActorHuman}
	if !o.HumanOnly {
		actors = append(actors, model.ActorAgent)
	}
	var out []store.TimelineEvent
	for _, a := range actors {
		evs, err := src.Timeline(ctx, store.TimelineFilter{SessionFilter: store.SessionFilter{Since: since, Until: until}, Actor: string(a), AllKinds: true})
		if err != nil {
			return nil, err
		}
		out = append(out, evs...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Event.TS.Before(out[j].Event.TS) })
	return out, nil
}

// key is the project key of an event after the configured definitions.
func key(e store.TimelineEvent, o Options) (string, bool) {
	k := o.Projects.Key(e.Project.Key, e.Project.Dir, e.Project.Remote)
	if k != e.Project.Key {
		return k, false
	}
	return k, e.Project.Dir
}

// attribute fills the blocks of the report from the events.
func (r *Report) attribute(events []store.TimelineEvent, o Options) error {
	first := r.Workday.First
	secs := func(t time.Time) int { return int(t.Sub(first).Seconds()) }
	names := nameMap(events, o)
	bi := 0
	var prev time.Time
	type cur struct {
		projects map[string]*BlockProject
		sessions map[string]*Segment
		order    []string
	}
	cs := make([]cur, len(r.Workday.Blocks))
	r.Blocks = make([]Block, len(r.Workday.Blocks))
	for i, b := range r.Workday.Blocks {
		r.Blocks[i].Block = b
		if i > 0 {
			r.Blocks[i].Pause = b.Start.Sub(r.Workday.Blocks[i-1].End)
		}
		cs[i] = cur{projects: map[string]*BlockProject{}, sessions: map[string]*Segment{}}
	}
	for _, e := range events {
		t := e.Event.TS
		for bi < len(r.Blocks) && t.After(r.Blocks[bi].End) {
			bi++
		}
		if bi >= len(r.Blocks) || t.Before(r.Blocks[bi].Start) {
			return fmt.Errorf("dayreport: event %s at %s is in no block", e.Event.ID, t.Format(time.RFC3339))
		}
		b, c := &r.Blocks[bi], &cs[bi]
		k, _ := key(e, o)
		p := c.projects[k]
		if p == nil {
			p = &BlockProject{Key: k, Name: names[k]}
			c.projects[k] = p
		}
		if t.Equal(b.Start) && b.Human+b.Agent == 0 {
			// the first event of the block counts no seconds
		} else {
			p.Secs += secs(t) - secs(prev)
		}
		prev = t
		s := c.sessions[e.Event.SessionID]
		if s == nil {
			s = &Segment{SessionID: e.Event.SessionID, Title: e.SessionName, Project: k, ProjectName: names[k], FirstEvent: e.Event.ID, Start: t}
			c.sessions[e.Event.SessionID] = s
			c.order = append(c.order, e.Event.SessionID)
		}
		s.End = t
		if e.Event.Actor == model.ActorHuman {
			b.Human++
			p.Human++
			s.Human++
		} else {
			b.Agent++
			p.Agent++
			s.Agent++
		}
	}
	for i := range r.Blocks {
		b, c := &r.Blocks[i], &cs[i]
		for _, p := range c.projects {
			b.Projects = append(b.Projects, *p)
		}
		sort.Slice(b.Projects, func(x, y int) bool {
			a, z := b.Projects[x], b.Projects[y]
			if a.Secs != z.Secs {
				return a.Secs > z.Secs
			}
			return a.Key < z.Key
		})
		for _, id := range c.order {
			b.Sessions = append(b.Sessions, *c.sessions[id])
		}
		if b.Human+b.Agent != b.Events {
			return fmt.Errorf("dayreport: block %s has %d events, counted %d", b.Start.Format("15:04"), b.Events, b.Human+b.Agent)
		}
		sum := 0
		for _, p := range b.Projects {
			sum += p.Secs
		}
		if sum != b.Secs {
			return fmt.Errorf("dayreport: block %s: project seconds %d, block %d", b.Start.Format("15:04"), sum, b.Secs)
		}
	}
	return nil
}

func nameMap(events []store.TimelineEvent, o Options) map[string]string {
	seen := map[string]bool{}
	var keys, dirs []string
	for _, e := range events {
		k, dir := key(e, o)
		if seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
		if dir {
			dirs = append(dirs, k)
		}
	}
	return o.Projects.Names(keys, dirs, o.Home)
}

// projectsOf merges the event counts and the worktime of the day by
// project key.
func projectsOf(events []store.TimelineEvent, wt worktime.Day, o Options) []Project {
	names := nameMap(events, o)
	by := map[string]*Project{}
	get := func(k, name string, dir bool) *Project {
		p := by[k]
		if p == nil {
			p = &Project{Key: k, Name: name, Dir: dir}
			by[k] = p
		}
		return p
	}
	for _, e := range events {
		k, dir := key(e, o)
		p := get(k, names[k], dir)
		if e.Event.Actor == model.ActorHuman {
			p.Human++
		} else {
			p.Agent++
		}
	}
	for _, c := range wt.Categories {
		for _, wp := range c.Projects {
			p := get(wp.Key, wp.Name, false)
			p.Raw += wp.Raw
			p.Rounded += wp.Rounded
		}
	}
	out := make([]Project, 0, len(by))
	for _, p := range by {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.Raw != b.Raw:
			return a.Raw > b.Raw
		case a.Human+a.Agent != b.Human+b.Agent:
			return a.Human+a.Agent > b.Human+b.Agent
		}
		return a.Name < b.Name
	})
	return out
}
