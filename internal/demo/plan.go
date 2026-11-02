package demo

import (
	"fmt"
	"time"

	"github.com/seletz/total-recall/internal/gitinfo"
	"github.com/seletz/total-recall/internal/holiday"
)

// session is one invented conversation: a main session or, below a main
// session, a subagent.
type session struct {
	id        string
	cwd       string
	title     string // custom title; empty means an AI title
	ai        string
	turns     []turn
	subagents []subagent
}

// subagent is a session spawned by a main session, with its agent name.
type subagent struct {
	id, name string
	session  session
}

// turn is one exchange: the prompt, the tool calls with their results and
// the reply, timed from the prompt. ids are the record uuids in file order:
// prompt, (tool_use, tool_result) per tool, reply.
type turn struct {
	at      time.Time
	prompt  string
	tools   []tool
	reply   string
	replyAt time.Duration
	ids     []string
}

// tool is one tool call with its result.
type tool struct {
	name, input, output string
}

// block is a window of the day in which sessions follow each other: the
// first prompt falls on start, the last one on end.
type block struct {
	start, end time.Time
}

// rng is a splitmix64 generator: the archive must not change with the Go
// version, so the standard library's generators are not used.
type rng struct{ s uint64 }

func (r *rng) next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// intn returns a number in [0, n); 0 for n < 1.
func (r *rng) intn(n int) int {
	if n < 1 {
		return 0
	}
	return int(r.next() % uint64(n))
}

// between returns a number in [lo, hi].
func (r *rng) between(lo, hi int) int { return lo + r.intn(hi-lo+1) }

// chance is true with the given probability in percent.
func (r *rng) chance(percent int) bool { return r.intn(100) < percent }

// pick returns a random element.
func pick[T any](r *rng, items []T) T { return items[r.intn(len(items))] }

// weighted returns a project by weight.
func weighted(r *rng, items []*project) *project {
	total := 0
	for _, p := range items {
		total += p.weight
	}
	n := r.intn(total)
	for _, p := range items {
		if n < p.weight {
			return p
		}
		n -= p.weight
	}
	return items[len(items)-1]
}

// planner turns the calendar into sessions.
type planner struct {
	r        *rng
	holidays *holiday.Calendar
	ids      int            // session counter
	events   int            // record counter
	titles   map[string]int // next title per project
	agents   int            // subagent counter
	focus    *project       // project of the current week
	focusKey string
}

// plan returns every session of the archive: the generated ones day by
// day, then the conversations written out by hand.
func plan() []session {
	p := &planner{r: &rng{s: 0x5EED_2026_0504}, holidays: holiday.New("Baden-Württemberg"), titles: map[string]int{}}
	var out []session
	for d := First; !d.After(Last); d = d.AddDate(0, 0, 1) {
		p.week(d)
		for _, b := range p.blocks(d) {
			out = append(out, p.sessions(b)...)
		}
	}
	for _, s := range featured() {
		s := s
		for i := range s.turns {
			s.turns[i].ids = p.turnIDs(len(s.turns[i].tools))
		}
		out = append(out, s)
	}
	return out
}

// week picks the focus project of the ISO week of d once per week.
func (p *planner) week(d time.Time) {
	y, w := d.ISOWeek()
	key := fmt.Sprintf("%d-%d", y, w)
	if key == p.focusKey {
		return
	}
	p.focusKey = key
	p.focus = weighted(p.r, projects)
}

// vacation is the two idle weeks in August.
func vacation(d time.Time) bool {
	return d.Month() == time.August && d.Day() >= 10 && d.Day() <= 23
}

// clock returns the instant at the given wall-clock time of the day; hours
// beyond 24 reach into the next day.
func clock(d time.Time, h, m int) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, Location)
}

// showcase is the hand-laid week of the activity screenshots: an ordinary
// day, a day past midnight, a short day, the worked holiday Fronleichnam,
// a short Friday, a Saturday and an idle Sunday.
var showcase = map[string][][4]int{
	"2026-06-01": {{8, 52, 17, 35}},
	"2026-06-02": {{9, 10, 18, 20}, {21, 40, 24, 45}},
	"2026-06-03": {{8, 30, 16, 5}},
	"2026-06-04": {{16, 30, 17, 40}},
	"2026-06-05": {{9, 30, 13, 15}},
	"2026-06-06": {{10, 15, 12, 40}},
	"2026-06-07": {},
}

// blocks returns the windows of activity of one day.
func (p *planner) blocks(d time.Time) []block {
	r := p.r
	if fixed, ok := showcase[d.Format("2006-01-02")]; ok {
		var out []block
		for _, f := range fixed {
			out = append(out, block{clock(d, f[0], f[1]), clock(d, f[2], f[3])})
		}
		return out
	}
	if vacation(d) {
		return nil
	}
	span := func(h, m, minDur, maxDur int) block {
		start := clock(d, h, m)
		return block{start, start.Add(time.Duration(r.between(minDur, maxDur)) * time.Minute)}
	}
	switch {
	case p.holidays.Is(d):
		if r.chance(35) {
			return []block{span(15, r.intn(90), 50, 90)}
		}
		return nil
	case d.Weekday() == time.Saturday:
		if r.chance(30) {
			return []block{span(10, r.intn(150), 60, 150)}
		}
		return nil
	case d.Weekday() == time.Sunday:
		if r.chance(15) {
			return []block{span(19, 30+r.intn(60), 45, 90)}
		}
		return nil
	}
	if r.chance(5) {
		return nil
	}
	start := clock(d, 8, 15+r.intn(95))
	end := clock(d, 16, 40+r.intn(110))
	if d.Weekday() == time.Friday {
		end = end.Add(-time.Duration(r.between(60, 90)) * time.Minute)
	}
	out := []block{{start, end}}
	switch {
	case r.chance(22):
		out = append(out, span(20, r.intn(75), 45, 120))
	case r.chance(10):
		out = append(out, span(22, 15+r.intn(60), 100, 200)) // past midnight
	}
	return out
}

// sessions fills a block with sessions one after the other, with short
// gaps; the first prompt falls on the start of the block, the last one on
// its end.
func (p *planner) sessions(b block) []session {
	var out []session
	cur := b.start
	for {
		dur := time.Duration(p.r.between(25, 110)) * time.Minute
		gap := time.Duration(p.r.between(5, 40)) * time.Minute
		if cur.Add(dur + gap + 20*time.Minute).After(b.end) {
			return append(out, p.session(cur, b.end.Sub(cur)))
		}
		out = append(out, p.session(cur, dur))
		cur = cur.Add(dur + gap)
	}
}

// session invents one conversation from start lasting dur: the turns are
// spread over the duration, the first at start and the last at its end.
func (p *planner) session(start time.Time, dur time.Duration) session {
	r := p.r
	pr := p.focus
	if !r.chance(55) {
		pr = weighted(r, projects)
	}
	p.ids++
	s := session{id: fmt.Sprintf("%08x-%04x-4000-8000-%012x", 0x7d1e0000+p.ids, p.ids, p.ids), cwd: pr.cwd}
	n := p.titles[pr.key]
	p.titles[pr.key] = n + 1
	if n%6 == 5 {
		s.title = pr.titles[n%len(pr.titles)]
	} else {
		s.ai = pr.titles[n%len(pr.titles)]
	}
	turns := 2 + int(dur/(25*time.Minute))
	if turns > 7 {
		turns = 7
	}
	// A heavy session is one where the agent works through many tool calls
	// per turn, so the volume of the days varies as it does in practice.
	heavy := r.chance(20)
	for i := range turns {
		at := start
		if i > 0 {
			at = start.Add(dur * time.Duration(i) / time.Duration(turns-1))
			if i < turns-1 {
				at = at.Add(time.Duration(r.between(-4, 4)) * time.Minute)
			}
		}
		calls := r.intn(4)
		if heavy {
			calls = r.between(3, 9)
		}
		var tools []tool
		for range calls {
			tools = append(tools, pick(r, pr.tools))
		}
		prompt := pick(r, pr.prompts)
		if r.chance(25) {
			prompt = pick(r, genericPrompts)
		}
		s.turns = append(s.turns, turn{
			at: at, prompt: prompt, tools: tools, reply: pick(r, pr.replies),
			replyAt: time.Duration(len(tools)+1)*time.Minute + time.Duration(r.intn(3))*time.Minute,
			ids:     p.turnIDs(len(tools)),
		})
	}
	if turns >= 3 && r.chance(15) {
		for range r.between(1, 2) {
			s.subagents = append(s.subagents, p.subagent(s, pr))
		}
	}
	return s
}

// subagent spawns a named subagent in one of the turns of s.
func (p *planner) subagent(s session, pr *project) subagent {
	r := p.r
	p.agents++
	kind := pick(r, agentKinds)
	parent := s.turns[1+r.intn(len(s.turns)-1)]
	var tools []tool
	for range r.between(1, 3) {
		tools = append(tools, pick(r, pr.tools))
	}
	return subagent{
		id:   fmt.Sprintf("%06x", 0xa0000+p.agents),
		name: kind.name,
		session: session{cwd: s.cwd, turns: []turn{{
			at: parent.at.Add(40 * time.Second), prompt: pick(r, kind.prompts), tools: tools,
			reply: pick(r, kind.replies), replyAt: time.Duration(len(tools)+2) * time.Minute,
			ids: p.turnIDs(len(tools)),
		}}},
	}
}

// turnIDs returns fresh record uuids for a turn with n tool calls.
func (p *planner) turnIDs(n int) []string {
	ids := make([]string, 0, 2+2*n)
	for range 2 + 2*n {
		p.events++
		ids = append(ids, fmt.Sprintf("e%06x", p.events))
	}
	return ids
}

// Git is the git resolution of the demo directories, to be used instead
// of real git: the repositories resolve to their root and remote, the
// worktree to its main repository, the notes directory to no repository.
func Git() map[string]gitinfo.Info {
	out := map[string]gitinfo.Info{}
	for _, p := range projects {
		if p.root == "" {
			continue
		}
		out[p.cwd] = gitinfo.Info{Dir: p.cwd, Toplevel: p.cwd, Root: p.root, RemoteURL: p.remote, Branch: p.branch}
	}
	return out
}
