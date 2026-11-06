package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/logfile"
	"github.com/seletz/total-recall/internal/ops"
	"github.com/seletz/total-recall/internal/store"
)

// HookPayload is the part of a harness hook's stdin JSON that ingest uses.
// Unknown fields are ignored.
type HookPayload struct {
	SessionID           string `json:"session_id"`
	TranscriptPath      string `json:"transcript_path"`
	Cwd                 string `json:"cwd"`
	HookEventName       string `json:"hook_event_name"`
	AgentTranscriptPath string `json:"agent_transcript_path"` // SubagentStop
}

func (e *env) ingestCmd() *cobra.Command {
	var all, hook, detach, regit, reread, rewriteRaw bool
	var payloadFile string
	c := &cobra.Command{
		Use:   "ingest [PATH...]",
		Short: "Read transcripts into the archive (incremental)",
		Long: `Read transcripts into the archive. Only new, complete lines are read.

  ingest PATH...           ingest the given transcript files
  ingest --all             ingest every source of every enabled harness
  ingest --hook            read a hook JSON payload from stdin and ingest its
                           transcript; on SessionStart also --all. Exit code is
                           always 0, errors go to the log file.
  ingest --regit           re-resolve git root and remote URL for the working
                           directory of every session (bypassing the cache)
                           and update the sessions; combinable with --all
  --reread                 read the given files (or, with --all, every source)
                           from the beginning instead of the stored offset.
                           Events are idempotent; sessions get what earlier
                           versions did not store (continuation, account)
  --rewrite-raw            with --reread: replace the stored raw line of every
                           event with the freshly redacted one (repairs raw
                           written by versions that masked whole values);
                           text, actor and the full-text index stay as they are
  --detach                 continue in a detached background process and
                           return immediately`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if hook {
				return e.runHook(cmd.Context(), detach, payloadFile)
			}
			if reread && !all && len(args) == 0 {
				return fmt.Errorf("ingest --reread: give PATH arguments or --all")
			}
			if rewriteRaw && !reread {
				return fmt.Errorf("ingest --rewrite-raw: needs --reread")
			}
			if !all && !regit && len(args) == 0 {
				return fmt.Errorf("ingest: give PATH arguments, --all, --regit or --hook")
			}
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			if detach {
				return e.app.StartDetached(childArgs(e.app.Args), cfg.LogPath())
			}
			return e.runIngest(cmd.Context(), cfg, args, ingestFlags{all: all, regit: regit, reread: reread, rewriteRaw: rewriteRaw})
		},
	}
	f := c.Flags()
	f.BoolVar(&all, "all", false, "ingest all sources of all enabled harnesses")
	f.BoolVar(&hook, "hook", false, "read hook JSON from stdin (always exits 0)")
	f.BoolVar(&regit, "regit", false, "re-resolve git info of all sessions (for stale git info)")
	f.BoolVar(&reread, "reread", false, "read the sources from the beginning again (fills session fields of older archives)")
	f.BoolVar(&rewriteRaw, "rewrite-raw", false, "with --reread: replace the stored raw line of existing events with the freshly redacted one")
	f.BoolVar(&detach, "detach", false, "run in a detached background process and return immediately")
	f.StringVar(&payloadFile, "hook-payload", "", "internal: read the hook payload from this file and delete it")
	_ = f.MarkHidden("hook-payload")
	return c
}

// warnOutdatedOnce logs, at WARN, that the hook wrote nothing because the
// archive needs a migration: once per schema pair and local day, remembered
// in a marker file in the lock dir, so the log does not fill up with one
// line per hook while the user has not migrated yet.
func warnOutdatedOnce(cfg *config.Config, log *logfile.Logger, now time.Time, event string, err error) {
	stamp := err.Error() + " " + now.In(cfg.Location()).Format("2006-01-02")
	marker := filepath.Join(ops.LockDir(cfg), "schema-warned")
	if b, rerr := os.ReadFile(marker); rerr == nil && strings.TrimSpace(string(b)) == stamp {
		return
	}
	if merr := os.MkdirAll(ops.LockDir(cfg), 0o755); merr == nil {
		_ = os.WriteFile(marker, []byte(stamp+"\n"), 0o644)
	}
	log.Warnf("hook %s: nothing written, the archive needs a migration (%v); hooks catch up afterwards", event, err)
}

// ingestFlags are the mode flags of `ingest`.
type ingestFlags struct {
	all, regit, reread, rewriteRaw bool
}

func (e *env) runIngest(ctx context.Context, cfg *config.Config, paths []string, fl ingestFlags) error {
	all, regit := fl.all, fl.regit
	if regit && !cfg.Git.Resolve {
		return fmt.Errorf("ingest --regit: git.resolve is disabled in the configuration")
	}
	if fl.rewriteRaw && !cfg.Ingest.StoreRaw {
		return fmt.Errorf("ingest --rewrite-raw: ingest.store_raw is disabled in the configuration, it would drop raw instead")
	}
	log := logfile.New(cfg.LogPath(), e.app.Now)
	st, err := ops.OpenWrite(cfg, log)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	d, err := ops.Deps(cfg, st, log)
	if err != nil {
		return err
	}
	d.Reread = fl.reread
	d.RewriteRaw = fl.rewriteRaw
	var total ingest.Summary
	if len(paths) > 0 {
		s, err := ingest.Run(ctx, d, paths)
		total = addSummary(total, s)
		if err != nil {
			return err
		}
	}
	if all {
		s, err := ingest.All(ctx, d)
		total = addSummary(total, s)
		if err != nil {
			return err
		}
	}
	out := e.out(cfg)
	if len(paths) > 0 || all {
		_, _ = fmt.Fprintln(out, ops.FormatIngest(total, fl.rewriteRaw))
	}
	if regit {
		rs, err := ingest.Regit(ctx, d)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "regit: %d directories, %d sessions updated (%s)\n", rs.Dirs, rs.Sessions, rs.Duration.Round(time.Millisecond))
	}
	if len(total.Errors) > 0 {
		w := e.app.Stderr
		if e.g.detachedChild {
			w = log.Writer("ERROR")
		}
		for _, fe := range total.Errors {
			_, _ = fmt.Fprintf(w, "error: %s: %v\n", fe.Path, fe.Err)
		}
		return fmt.Errorf("%d of %d files failed", len(total.Errors), total.Scanned)
	}
	return nil
}

func addSummary(a, b ingest.Summary) ingest.Summary {
	a.Scanned += b.Scanned
	a.Changed += b.Changed
	a.Unchanged += b.Unchanged
	a.Locked += b.Locked
	a.Events += b.Events
	a.RawRewritten += b.RawRewritten
	a.Meta += b.Meta
	a.MetaKept += b.MetaKept
	a.Blobs += b.Blobs
	a.BlobBytes += b.BlobBytes
	a.Sessions += b.Sessions
	a.Errors = append(a.Errors, b.Errors...)
	a.Duration += b.Duration
	return a
}

// runHook implements `ingest --hook`. It never returns an error: hooks must
// not disturb the harness. Problems are written to the log file (or stderr
// if not even the configuration can be loaded).
func (e *env) runHook(ctx context.Context, detach bool, payloadFile string) error {
	cfg, err := e.loadConfig()
	if err != nil {
		_, _ = fmt.Fprintln(e.app.Stderr, "total-recall: hook:", err)
		return nil
	}
	log := logfile.New(cfg.LogPath(), e.app.Now)
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("hook: panic: %v", r)
		}
	}()

	var raw []byte
	if payloadFile != "" {
		raw, err = os.ReadFile(payloadFile)
		_ = os.Remove(payloadFile)
	} else {
		raw, err = io.ReadAll(io.LimitReader(e.app.Stdin, 1<<20))
	}
	if err != nil {
		log.Errorf("hook: read payload: %v", err)
		return nil
	}

	if detach {
		f, err := os.CreateTemp("", "total-recall-hook-*.json")
		if err != nil {
			log.Errorf("hook: detach: %v", err)
			return nil
		}
		_, werr := f.Write(raw)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			_ = os.Remove(f.Name())
			log.Errorf("hook: detach: write payload: %v %v", werr, cerr)
			return nil
		}
		if err := e.app.StartDetached(childArgs(e.app.Args, "--hook-payload", f.Name()), cfg.LogPath()); err != nil {
			_ = os.Remove(f.Name())
			log.Errorf("hook: detach: %v", err)
		}
		return nil
	}

	var p HookPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		log.Errorf("hook: invalid payload JSON: %v", err)
		return nil
	}
	var paths []string
	for _, t := range []string{p.TranscriptPath, p.AgentTranscriptPath} {
		if t != "" {
			paths = append(paths, t)
		}
	}
	sessionStart := p.HookEventName == "SessionStart"
	if len(paths) == 0 && !sessionStart {
		log.Errorf("hook %s (session %s): no transcript_path in payload", p.HookEventName, p.SessionID)
		return nil
	}

	st, err := ops.OpenWrite(cfg, log)
	if errors.Is(err, store.ErrMaintenance) {
		// Nothing is lost: the next hook of the session (or SessionStart
		// with --all) reads on from the stored offset.
		log.Infof("hook %s: maintenance in progress, skipped; the next hook catches up", p.HookEventName)
		return nil
	}
	if errors.Is(err, store.ErrSchemaOutdated) {
		// Same here: the stored offsets are untouched, the first hook after
		// `total-recall migrate` reads everything that was skipped.
		warnOutdatedOnce(cfg, log, e.app.Now(), p.HookEventName, err)
		return nil
	}
	if err != nil {
		log.Errorf("hook %s: %v", p.HookEventName, err)
		return nil
	}
	defer func() { _ = st.Close() }()
	d, err := ops.Deps(cfg, st, log)
	if err != nil {
		log.Errorf("hook %s: %v", p.HookEventName, err)
		return nil
	}
	report := func(s ingest.Summary, err error) {
		for _, fe := range s.Errors {
			// The harness announces files it has not written (yet): a new
			// session's SessionStart, internal agents without a transcript
			// on SubagentStop. A later hook picks them up if they appear (#7).
			if errors.Is(fe.Err, fs.ErrNotExist) {
				log.Infof("hook %s: transcript not present, skipped: %s", p.HookEventName, fe.Path)
				continue
			}
			log.Errorf("hook %s: %s: %v", p.HookEventName, fe.Path, fe.Err)
		}
		if err != nil {
			log.Errorf("hook %s: %v", p.HookEventName, err)
		}
	}
	if len(paths) > 0 {
		report(ingest.Run(ctx, d, paths))
	}
	if sessionStart {
		report(ingest.All(ctx, d))
	}
	return nil
}
