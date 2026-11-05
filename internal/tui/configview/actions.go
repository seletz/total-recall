package configview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/logfile"
	"github.com/seletz/total-recall/internal/ops"
	"github.com/seletz/total-recall/internal/store"
)

// runEnv is what an action gets: the configuration, the clock and the
// options, copied before the action's goroutine starts.
type runEnv struct {
	cfg  *config.Config
	opts Options
	now  time.Time
}

// logf receives the progress lines of an action.
type logf func(format string, args ...any)

// action is one entry of the actions page.
type action struct {
	name    string
	summary string
	// writes marks an action that changes the archive: it asks for a
	// confirmation and pauses the reload.
	writes bool
	// confirm describes what the action is about to do; prompt replaces it
	// when that depends on the moment of the request.
	confirm string
	prompt  func() string
	// disabled says why the action cannot run right now; empty runs.
	disabled string
	// details are shown under the list while the action waits for its
	// confirmation (the plan of a migration).
	details []string
	run     func(ctx context.Context, env runEnv, log logf) (string, error)
}

// Messages of a run: one progress line, then the result.
type (
	lineMsg struct {
		seq  uint64
		line string
	}
	doneMsg struct {
		seq    uint64
		result string
		err    error
		took   time.Duration
	}
)

// actionsPage lists the actions and runs one at a time, showing its
// progress lines below the list.
type actionsPage struct {
	row     int
	confirm bool // waiting for y
	prompt  string
	running bool
	seq     uint64
	cancel  context.CancelFunc
	events  chan tea.Msg
	// current is the action that runs or ran last; log its lines.
	current string
	log     []string
	result  string
	err     error
	took    time.Duration
}

func newActionsPage() actionsPage { return actionsPage{} }

func (actionsPage) title() string { return "actions" }

func (actionsPage) focused() bool { return false }

func (p actionsPage) shortHelp(m *Model) []key.Binding {
	switch {
	case p.running:
		return []key.Binding{m.keys.Back}
	case p.confirm:
		return []key.Binding{m.keys.Confirm, m.keys.Back}
	}
	return []key.Binding{m.keys.Move, m.keys.Select}
}

func (p actionsPage) fullHelp(m *Model) [][]key.Binding {
	return [][]key.Binding{{m.keys.Up, m.keys.Down, m.keys.Select}, {m.keys.Confirm, m.keys.Back}}
}

func (p actionsPage) reload(*Model) (page, tea.Cmd) { return p, nil }

// actions lists the actions for the current state.
func (m Model) actions() []action {
	cfg, s := m.opts.Config, m.state
	if cfg == nil {
		return nil
	}
	dir := m.tilde(cfg.Backup.Dir)
	noArchive := ""
	if s.loaded && s.noArchive {
		noArchive = "no archive yet"
	}
	// Until the migration ran, nothing but backup writes to the archive.
	outdated := noArchive
	if outdated == "" && s.pending() {
		outdated = "migrate first"
	}
	migrate := action{name: "migrate", writes: true,
		summary: "apply the pending schema migrations after a backup; hooks skip meanwhile",
		run:     runMigrate}
	switch {
	case !s.loaded:
		migrate.disabled = "loading"
	case noArchive != "":
		migrate.disabled = noArchive
	case !s.pending():
		migrate.summary, migrate.disabled = fmt.Sprintf("schema v%d is current", s.schema), "nothing pending"
	default:
		migrate.confirm = fmt.Sprintf("migrate from schema v%d to v%d after a copy (%s)", s.plan.From, s.plan.To, filepath.Base(s.plan.Backup))
		plan := s.plan
		plan.Backup = m.tilde(plan.Backup)
		migrate.details = ops.PlanLines(plan)
		if s.backup == "" {
			migrate.summary = "apply the pending schema migrations; no backup yet, run backup first"
		}
	}
	return []action{
		{name: "backup", writes: true, disabled: noArchive,
			summary: fmt.Sprintf("write a compressed copy to %s, keep %d daily and %d monthly", dir, cfg.Backup.KeepDaily, cfg.Backup.KeepMonthly),
			prompt: func() string {
				name, err := store.NextBackupName(cfg.Backup.Dir, m.opts.Now().In(cfg.Location()))
				if err != nil {
					name = "a new copy"
				}
				return fmt.Sprintf("write %s to %s and prune older copies", name, dir)
			},
			run: runBackup},
		{name: "maintenance (dry run)", disabled: outdated,
			summary: fmt.Sprintf("measure a rebuild with %d-byte pages; changes nothing", ops.RecommendedPageSize),
			run:     runMaintain(true)},
		{name: "maintenance", writes: true, disabled: outdated,
			summary: fmt.Sprintf("rebuild the archive with %d-byte pages after a backup; hooks skip meanwhile", ops.RecommendedPageSize),
			confirm: fmt.Sprintf("back up to %s, rebuild %s with %d-byte pages and swap it in (needs about twice its size free)", dir, m.tilde(cfg.DB), ops.RecommendedPageSize),
			run:     runMaintain(false)},
		migrate,
		{name: "reindex actors (dry run)", disabled: outdated,
			summary: "count the stored user messages whose actor the current rules would change",
			run:     runReindex(true)},
		{name: "reindex actors", writes: true, disabled: outdated,
			summary: "apply the current actor rules to every stored user message",
			confirm: "reclassify the actor of every stored user message with the rules of the configuration",
			run:     runReindex(false)},
		{name: "ingest all", writes: true, disabled: outdatedOnly(s, outdated),
			summary: "read every transcript of every harness from the stored offset",
			confirm: "read every transcript of every enabled harness into " + m.tilde(cfg.DB),
			run:     runIngest},
		{name: "doctor",
			summary: "check the transcripts for format drift; reads only",
			run:     runDoctor},
	}
}

// outdatedOnly is the reason for an action that works without an archive
// but not with an outdated one: "" unless the migration is pending.
func outdatedOnly(s state, reason string) string {
	if s.pending() {
		return reason
	}
	return ""
}

func (p actionsPage) update(m *Model, msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case lineMsg:
		if msg.seq != p.seq {
			return p, nil
		}
		p.log = append(p.log, msg.line)
		return p, p.wait()
	case doneMsg:
		if msg.seq != p.seq {
			return p, nil
		}
		return p.done(m, msg)
	case tea.KeyPressMsg:
		return p.key(m, msg)
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || p.running || p.confirm {
			return p, nil
		}
		i := msg.Y - 1
		if i < 0 || i >= len(m.actions()) {
			return p, nil
		}
		if i == p.row {
			return p.selectRow(m)
		}
		p.row = i
		return p, nil
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			return p.move(m, -1), nil
		}
		return p.move(m, 1), nil
	}
	return p, nil
}

func (p actionsPage) key(m *Model, msg tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case p.running:
		if key.Matches(msg, m.keys.Back) && p.cancel != nil {
			p.cancel()
			p.log = append(p.log, "cancelling…")
		}
		return p, nil
	case p.confirm:
		switch {
		case key.Matches(msg, m.keys.Confirm):
			p.confirm = false
			return p.start(m, m.actions()[p.row])
		case key.Matches(msg, m.keys.Back):
			p.confirm = false
			p.log = []string{p.current + ": cancelled"}
		}
		return p, nil
	case key.Matches(msg, m.keys.Up):
		return p.move(m, -1), nil
	case key.Matches(msg, m.keys.Down):
		return p.move(m, 1), nil
	case key.Matches(msg, m.keys.Select):
		return p.selectRow(m)
	}
	return p, nil
}

func (p actionsPage) move(m *Model, dir int) actionsPage {
	if p.running || p.confirm {
		return p
	}
	n := len(m.actions())
	if n == 0 {
		return p
	}
	p.row = min(max(p.row+dir, 0), n-1)
	return p
}

// selectRow asks for a confirmation of a writing action, runs a reading
// one right away.
func (p actionsPage) selectRow(m *Model) (page, tea.Cmd) {
	acts := m.actions()
	if p.row >= len(acts) {
		return p, nil
	}
	a := acts[p.row]
	p.log, p.result, p.err, p.current = nil, "", nil, a.name
	if a.disabled != "" {
		p.err = errors.New(a.disabled)
		return p, nil
	}
	if a.writes {
		p.confirm, p.prompt = true, a.confirm
		if a.prompt != nil {
			p.prompt = a.prompt()
		}
		p.log = a.details
		return p, nil
	}
	return p.start(m, a)
}

// start runs a in a goroutine; its progress lines and result arrive as
// messages. A writing action tells the app it is busy.
func (p actionsPage) start(m *Model, a action) (page, tea.Cmd) {
	p.seq++
	seq := p.seq
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan tea.Msg, 256)
	env := runEnv{cfg: m.opts.Config, opts: m.opts, now: m.opts.Now()}
	p.cancel, p.events, p.running, p.current = cancel, events, true, a.name
	p.log, p.result, p.err = nil, "", nil
	go func() {
		start := time.Now()
		log := func(format string, args ...any) {
			events <- lineMsg{seq: seq, line: fmt.Sprintf(format, args...)}
		}
		res, err := a.run(ctx, env, log)
		events <- doneMsg{seq: seq, result: res, err: err, took: time.Since(start)}
		close(events)
	}()
	if a.writes {
		// Busy first, so the app pauses before the first result lands.
		return p, tea.Sequence(func() tea.Msg { return Busy{On: true} }, p.wait())
	}
	return p, p.wait()
}

// wait delivers the next message of the running action.
func (p actionsPage) wait() tea.Cmd {
	events := p.events
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return nil
		}
		return msg
	}
}

// done records the outcome and releases the app.
func (p actionsPage) done(m *Model, msg doneMsg) (page, tea.Cmd) {
	p.running = false
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.result, p.err, p.took = msg.result, msg.err, msg.took
	for _, a := range m.actions() {
		if a.name == p.current && a.writes {
			return p, func() tea.Msg { return Busy{On: false} }
		}
	}
	return p, nil
}

// view renders the list, a rule, the status line and the log.
func (p actionsPage) view(m *Model, width, height int) string {
	acts := m.actions()
	if len(acts) == 0 {
		return " " + m.th.Muted.Render("no configuration")
	}
	nameW := 0
	for _, a := range acts {
		nameW = max(nameW, len(a.name))
	}
	lines := make([]string, 0, height)
	lines = append(lines, m.line(" "+m.th.Muted.Render(pad("ACTION", nameW+1)+" WHAT IT DOES"), width))
	for i, a := range acts {
		mark := " "
		if i == p.row {
			mark = m.th.Mark.Render("▸")
		}
		name := pad(a.name, nameW)
		summary := a.summary
		switch {
		case a.disabled != "":
			name, summary = m.th.Muted.Render(name), m.th.Muted.Render(summary+" · "+a.disabled)
		case a.writes:
			name = m.th.Strong.Render(name)
		}
		line := name + "  " + summary
		if i == p.row {
			line = m.th.Selected.Render(pad(line, width-2))
		}
		lines = append(lines, m.line(mark+" "+line, width))
	}
	lines = append(lines, m.th.Rule.Render(strings.Repeat("─", width)))
	status := m.th.Muted.Render("enter runs the marked action; actions that write ask first")
	switch {
	case p.running:
		status = m.th.Spinner.Render("⠿") + " " + p.current + " running… " + m.th.Muted.Render("esc cancels")
	case p.confirm:
		status = m.th.Strong.Render(p.current+":") + " " + p.prompt
		lines = append(lines, m.line(" "+status, width))
		status = m.th.AccentText.Render("y confirms, esc cancels")
	case p.err != nil:
		status = m.th.Error.Render(p.current + ": " + p.err.Error())
	case p.result != "":
		status = m.th.Strong.Render(p.current+":") + " " + p.result + "  " + m.th.Muted.Render(fmt.Sprintf("(%s)", p.took.Round(time.Millisecond)))
	}
	lines = append(lines, m.line(" "+status, width))
	avail := height - len(lines)
	log := p.log
	if len(log) > avail {
		log = log[len(log)-max(avail, 0):]
	}
	for _, l := range log {
		lines = append(lines, m.line("   "+m.th.Muted.Render(l), width))
	}
	return strings.Join(lines, "\n")
}

// ---- the actions, in process, with the functions of the CLI ----

// openWrite opens the archive for writing as the CLI does, logging to the
// configured log file.
func openWrite(env runEnv, extra ...store.Option) (*store.Store, *logfile.Logger, error) {
	log := logfile.New(env.cfg.LogPath(), env.opts.Now)
	st, err := ops.OpenWrite(env.cfg, log, extra...)
	return st, log, err
}

func runBackup(ctx context.Context, env runEnv, log logf) (string, error) {
	// backup is the step before a migration, so it accepts an older schema.
	st, _, err := openWrite(env, store.AllowOutdated())
	if err != nil {
		return "", err
	}
	defer func() { _ = st.Close() }()
	now := env.now.In(env.cfg.Location())
	if name, err := store.NextBackupName(env.cfg.Backup.Dir, now); err == nil {
		log("writing %s …", name)
	}
	p, err := st.Backup(ctx, env.cfg.Backup.Dir, now, env.cfg.Backup.KeepDaily, env.cfg.Backup.KeepMonthly)
	if err != nil {
		return "", err
	}
	return "backup written: " + filepath.Base(p), nil
}

// progressInterval is how often a running migration reports its progress.
const progressInterval = 2 * time.Second

func runMigrate(ctx context.Context, env runEnv, log logf) (string, error) {
	var last time.Time
	progress := func(p store.Progress) {
		if p.Total == 0 {
			return
		}
		t := time.Now()
		if p.Done < p.Total && t.Sub(last) < progressInterval {
			return
		}
		last = t
		log("migration v%d: %d/%d (%d %%)", p.Version, p.Done, p.Total, p.Done*100/p.Total)
	}
	res, err := ops.Migrate(ctx, env.cfg, env.opts.Wait, func(f string, a ...any) { log(f, a...) }, progress)
	if err != nil {
		return "", err
	}
	if !res.Plan.Pending() {
		return fmt.Sprintf("schema v%d is current, nothing to migrate", res.Plan.From), nil
	}
	return fmt.Sprintf("migrated v%d -> v%d; the copy %s can be removed once everything works", res.Plan.From, res.Plan.To, filepath.Base(res.Plan.Backup)), nil
}

func runMaintain(dryRun bool) func(context.Context, runEnv, logf) (string, error) {
	return func(ctx context.Context, env runEnv, log logf) (string, error) {
		o := ops.MaintainOptions(env.cfg, env.now, 0, dryRun, env.opts.Wait)
		file := logfile.New(env.cfg.LogPath(), env.opts.Now)
		o.Logf = func(format string, args ...any) {
			file.Infof(format, args...)
			log(format, args...)
		}
		res, err := store.Maintain(ctx, env.cfg.DB, o)
		if err != nil {
			return "", err
		}
		verb := "rebuilt"
		if res.DryRun {
			verb = "would be rebuilt"
		}
		return fmt.Sprintf("%s: %s → %s, page size %d → %d", verb, mib(res.Before.SizeBytes), mib(res.After.SizeBytes), res.Before.PageSize, res.After.PageSize), nil
	}
}

func runReindex(dryRun bool) func(context.Context, runEnv, logf) (string, error) {
	return func(ctx context.Context, env runEnv, _ logf) (string, error) {
		st, lg, err := openWrite(env)
		if err != nil {
			return "", err
		}
		defer func() { _ = st.Close() }()
		d, err := ops.Deps(env.cfg, st, lg)
		if err != nil {
			return "", err
		}
		sum, err := ingest.Reclassify(ctx, d, dryRun)
		if err != nil {
			return "", err
		}
		return ops.FormatReclassify(sum), nil
	}
}

func runIngest(ctx context.Context, env runEnv, log logf) (string, error) {
	st, lg, err := openWrite(env)
	if err != nil {
		return "", err
	}
	defer func() { _ = st.Close() }()
	d, err := ops.Deps(env.cfg, st, lg)
	if err != nil {
		return "", err
	}
	sum, err := ingest.All(ctx, d)
	for _, fe := range sum.Errors {
		log("error: %s: %v", fe.Path, fe.Err)
	}
	if err != nil {
		return "", err
	}
	if len(sum.Errors) > 0 {
		return "", fmt.Errorf("%d of %d files failed", len(sum.Errors), sum.Scanned)
	}
	return ops.FormatIngest(sum, false), nil
}

func runDoctor(ctx context.Context, env runEnv, log logf) (string, error) {
	clean := true
	n := 0
	for _, a := range ops.Adapters(env.cfg) {
		fc, ok := a.(harness.FormatChecker)
		if !ok {
			continue
		}
		n++
		r, err := fc.CheckFormat(ctx)
		if err != nil {
			return "", err
		}
		var b bytes.Buffer
		ops.FormatReport(&b, r)
		for _, l := range strings.Split(strings.TrimRight(b.String(), "\n"), "\n") {
			log("%s", l)
		}
		clean = clean && r.Clean()
	}
	if n == 0 {
		return "", errors.New("no enabled harness with a format check")
	}
	if !clean {
		return "findings: the adapter may need an update", nil
	}
	return "clean: no unknown record types, content blocks or missing fields", nil
}

func (actionsPage) cursor(int) *tea.Cursor { return nil }
