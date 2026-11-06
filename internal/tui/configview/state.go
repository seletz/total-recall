package configview

import (
	"context"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/logfile"
	"github.com/seletz/total-recall/internal/store"
)

// errorWindow is the age up to which log errors are listed, as in status.
const errorWindow = 24 * time.Hour

// state is what the status page shows and the other pages consult: the
// archive as `status` reports it, the problems of the configuration and
// the errors of the log.
type state struct {
	// loaded is true once the archive was read; err is the error of the
	// load, if any.
	loaded bool
	err    error
	// noArchive is true when the configured database does not exist.
	noArchive bool
	size      int64
	pages     store.PageStats
	schema    int
	latest    int
	// plan is what migrate would do, loaded when a migration is pending.
	plan store.MigratePlan
	// raw counts the stored raw lines and their size; not read while a
	// migration is pending (the columns may not exist yet).
	raw store.RawStats
	// blobs counts the stored attachments; not read while a migration is
	// pending either.
	blobs store.BlobStats
	// meta counts the stored meta records (session_meta) and their
	// compressed size; not read while a migration is pending either.
	meta   store.MetaStats
	status store.Status
	// backup is the newest backup copy and its time; empty without one.
	backup   string
	backupAt time.Time
	// problems are the errors of the config tables checked when used.
	problems []error
	// errs are the ERROR lines of the log of the last errorWindow,
	// oldest first; older counts the ones before.
	errs  []logfile.Error
	older int
}

// pending reports whether the archive is behind the binary's schema.
func (s state) pending() bool { return s.loaded && s.err == nil && !s.noArchive && s.schema < s.latest }

// stateMsg carries a loaded state.
type stateMsg struct {
	seq   uint64
	state state
}

// stateCmd reads the status in a command; the result carries the current
// sequence number, so an older load that lands late is dropped. Callers
// that start a new load bump the sequence first (Init cannot and uses the
// initial one).
func (m Model) stateCmd() tea.Cmd {
	seq, opts, now := m.seq, m.opts, m.opts.Now()
	return func() tea.Msg {
		return stateMsg{seq: seq, state: load(opts, now)}
	}
}

// loadState starts a new load of the status.
func (m *Model) loadState() tea.Cmd {
	m.seq++
	m.loading = true
	return m.stateCmd()
}

// load gathers the state: counts, versions and page stats from the store,
// the backup from the backup directory, errors from the log, problems from
// the configuration.
func load(opts Options, now time.Time) state {
	s := state{loaded: true, latest: store.LatestSchemaVersion()}
	cfg := opts.Config
	if cfg == nil {
		return s
	}
	s.problems = cfg.Problems()
	if errs, err := logfile.ReadErrors(cfg.LogPath()); err != nil {
		s.err = err
	} else {
		s.errs, s.older = logfile.SplitErrors(errs, now.Add(-errorWindow))
	}
	if p, t, err := store.LastBackup(cfg.Backup.Dir, cfg.Location()); err != nil {
		s.err = err
	} else {
		s.backup, s.backupAt = p, t
	}
	if _, err := os.Stat(cfg.DB); err != nil {
		s.noArchive = true
	}
	if opts.Store == nil {
		return s
	}
	ctx := context.Background()
	st, err := opts.Store.Status(ctx)
	if err != nil {
		s.err = err
		return s
	}
	s.status = st
	if s.schema, err = opts.Store.SchemaVersion(ctx); err != nil {
		s.err = err
		return s
	}
	if s.pages, err = opts.Store.PageStats(ctx); err != nil {
		s.err = err
		return s
	}
	s.size = s.pages.SizeBytes
	if s.pending() {
		if s.plan, err = store.PlanMigration(ctx, cfg.DB); err != nil {
			s.err = err
		}
		return s
	}
	if s.raw, err = opts.Store.RawStats(ctx); err != nil {
		s.err = err
	}
	if s.blobs, err = opts.Store.BlobStats(ctx); err != nil {
		s.err = err
	}
	if s.meta, err = opts.Store.MetaStats(ctx); err != nil {
		s.err = err
	}
	return s
}
