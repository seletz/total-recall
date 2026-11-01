package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/harness/claude"
	"github.com/seletz/total-recall/internal/ingest"
	"github.com/seletz/total-recall/internal/redact"
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
	var all, hook, detach, regit bool
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
  --detach                 continue in a detached background process and
                           return immediately`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if hook {
				return e.runHook(cmd.Context(), detach, payloadFile)
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
			return e.runIngest(cmd.Context(), cfg, args, all, regit)
		},
	}
	f := c.Flags()
	f.BoolVar(&all, "all", false, "ingest all sources of all enabled harnesses")
	f.BoolVar(&hook, "hook", false, "read hook JSON from stdin (always exits 0)")
	f.BoolVar(&regit, "regit", false, "re-resolve git info of all sessions (for stale git info)")
	f.BoolVar(&detach, "detach", false, "run in a detached background process and return immediately")
	f.StringVar(&payloadFile, "hook-payload", "", "internal: read the hook payload from this file and delete it")
	_ = f.MarkHidden("hook-payload")
	return c
}

// deps builds the ingest dependencies from the configuration.
func deps(cfg *config.Config, st *store.Store, log *logger) (ingest.Deps, error) {
	d := ingest.Deps{
		Store:       st,
		StoreRaw:    cfg.Ingest.StoreRaw,
		RawMaxBytes: cfg.Ingest.RawMaxBytes,
		ResolveGit:  cfg.Git.Resolve,
		LockDir:     filepath.Join(filepath.Dir(cfg.DB), "locks"),
		Logf:        func(f string, a ...any) { log.Infof(f, a...) },
	}
	if cfg.Redact.Enabled {
		r, err := redact.New(redact.Options{ExtraPatterns: cfg.Redact.ExtraPatterns, Allow: cfg.Redact.Allow})
		if err != nil {
			return d, err
		}
		d.Redactor = r
	}
	d.Adapters = adapters(cfg)
	return d, nil
}

// adapters returns the enabled harness adapters configured from cfg.
func adapters(cfg *config.Config) []harness.Adapter {
	var out []harness.Adapter
	if c := cfg.Harness.Claude; c.Enabled {
		out = append(out, claude.New(c.ProjectsDir,
			claude.WithToolResultMaxBytes(cfg.Ingest.ToolResultMaxBytes),
			claude.WithPeerPrefixes(c.Actors.PeerPrefixes...),
			claude.WithAutomationPrefixes(c.Actors.AutomationPrefixes...),
			claude.WithSkipRecordTypes(c.SkipRecordTypes...),
		))
	}
	return out
}

// openWrite opens the archive read-write (migrating it if needed).
func openWrite(cfg *config.Config, log *logger) (*store.Store, error) {
	return store.Open(cfg.DB, store.WithLocation(cfg.Location()),
		store.WithLogf(func(f string, a ...any) { log.Infof(f, a...) }))
}

func (e *env) runIngest(ctx context.Context, cfg *config.Config, paths []string, all, regit bool) error {
	if regit && !cfg.Git.Resolve {
		return fmt.Errorf("ingest --regit: git.resolve is disabled in the configuration")
	}
	log := newLogger(cfg.LogPath(), e.app.Now)
	st, err := openWrite(cfg, log)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	d, err := deps(cfg, st, log)
	if err != nil {
		return err
	}
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
		_, _ = fmt.Fprintf(out, "scanned %d files: %d changed, %d unchanged, %d locked; %d new events, %d sessions touched (%s)\n",
			total.Scanned, total.Changed, total.Unchanged, total.Locked, total.Events, total.Sessions, total.Duration.Round(time.Millisecond))
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
			w = &logWriter{l: log, level: "ERROR"}
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
	log := newLogger(cfg.LogPath(), e.app.Now)
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

	st, err := openWrite(cfg, log)
	if err != nil {
		log.Errorf("hook %s: %v", p.HookEventName, err)
		return nil
	}
	defer func() { _ = st.Close() }()
	d, err := deps(cfg, st, log)
	if err != nil {
		log.Errorf("hook %s: %v", p.HookEventName, err)
		return nil
	}
	report := func(s ingest.Summary, err error) {
		for _, fe := range s.Errors {
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
