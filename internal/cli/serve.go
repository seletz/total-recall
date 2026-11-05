package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/version"
	"github.com/seletz/total-recall/internal/web"
)

// serveFlags are the flags of serve.
type serveFlags struct {
	port  int
	token string
	open  bool
	poll  time.Duration
}

// DefaultServePort is the port of serve without --port.
const DefaultServePort = 8787

func (e *env) serveCmd() *cobra.Command {
	var f serveFlags
	c := &cobra.Command{
		Use:   "serve",
		Short: "Serve the web UI (search, activity statistics) on 127.0.0.1 [prototype]",
		Long: `serve opens the archive read-only and serves the embedded web UI and its
JSON API on the loopback interface. It prints the address with a one-time
token in the URL fragment; open that URL. Requests from other hosts, other
origins or without the token are refused, and the page loads nothing from
the network.

The UI is a prototype (spike): a live full-text search and a chart of the
activity per period and project or agent. It reloads the view on screen
when the hooks write to the archive (polled every --poll).

Stop with Ctrl+C.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if f.port < 0 || f.port > 65535 {
				return usagef("--port must be between 0 and 65535, not %d", f.port)
			}
			if f.poll < time.Second {
				return usagef("--poll must be at least 1s")
			}
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			st, err := e.openRead(cfg)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			token := f.token
			if token == "" {
				if token, err = randomToken(); err != nil {
					return err
				}
			}
			srv, err := web.New(e.serveOptions(cfg, st, token, f.poll))
			if err != nil {
				return err
			}
			ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", f.port))
			if err != nil {
				return fmt.Errorf("listen: %w", err)
			}
			url := fmt.Sprintf("http://%s/#token=%s", ln.Addr(), token)
			_, _ = fmt.Fprintf(e.out(cfg), "total-recall serve: %s\n", url)
			if f.open {
				if err := openBrowser(url); err != nil {
					_, _ = fmt.Fprintln(e.app.Stderr, "warning: open browser:", err)
				}
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return srv.Serve(ctx, ln)
		},
	}
	c.Flags().IntVar(&f.port, "port", DefaultServePort, "port on 127.0.0.1; 0 picks a free one")
	c.Flags().StringVar(&f.token, "token", "", "token the UI must send (default: random per run; for development)")
	c.Flags().BoolVar(&f.open, "open", false, "open the URL in the default browser")
	c.Flags().DurationVar(&f.poll, "poll", 2*time.Second, "interval at which the archive is checked for changes (live updates)")
	return c
}

// randomToken returns 128 random bits in hex.
func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// openBrowser opens url with the platform's opener.
func openBrowser(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	return c.Start()
}

// serveOptions wires the web API to the store: the handlers answer with
// the same JSON structs as search --json, stats --period --by --json and
// status, so the UI reads the contract the CLI already has.
func (e *env) serveOptions(cfg *config.Config, st *store.Store, token string, poll time.Duration) web.Options {
	loc := cfg.Location()
	return web.Options{
		Token: token, Version: version.Version, Location: loc, Watcher: st, PollInterval: poll, Log: e.app.Stderr,
		Status: func(ctx context.Context) (web.Status, error) {
			s, err := st.Status(ctx)
			if err != nil {
				return web.Status{}, err
			}
			out := web.Status{Version: version.Version, Sessions: s.Sessions, Events: s.Events, Sources: s.Sources,
				LastIngest: map[string]string{}, SchemaVersion: st.Version(), MigrationPending: st.MigrationPending(), Timezone: cfg.Timezone}
			for h, t := range s.LastIngest {
				out.LastIngest[h] = isoTime(t)
			}
			return out, nil
		},
		Search: func(ctx context.Context, q web.SearchQuery) (any, error) {
			hits, err := st.Search(ctx, q.Query, store.SearchFilter{
				SessionFilter:  store.SessionFilter{Since: q.Since, Until: q.Until, Limit: q.Limit},
				Actor:          q.Actor,
				Raw:            q.Raw,
				HighlightStart: q.HighlightStart, HighlightEnd: q.HighlightEnd,
			})
			if err != nil {
				return nil, err
			}
			return hitsJSON(hits), nil
		},
		Activity: func(ctx context.Context, q web.ActivityQuery) (any, error) {
			opts, err := parseSeriesOptions(q.Period, q.By, q.Top)
			if err != nil {
				return nil, web.Usage(err)
			}
			since, until := q.Since, q.Until
			if until.IsZero() {
				until = e.app.Now().In(loc)
			}
			if since.IsZero() {
				since = defaultSeriesStart(opts.period, until, loc)
			}
			var projects project.Resolver
			if opts.by == store.DimProject && opts.byName != "" {
				if projects, err = cfg.ProjectResolver(); err != nil {
					return nil, err
				}
			}
			as, err := st.ActivitySeries(ctx, store.ActivityFilter{
				Since: since, Until: until, Location: loc,
				Period: opts.period, Dimension: opts.by, TopN: opts.top, Project: projects.Key,
			})
			if err != nil {
				return nil, err
			}
			return seriesJSON(as, opts, keyNames(as, opts, "", cfg.Accounts, projects), since, until, cfg.Timezone), nil
		},
	}
}

// defaultSeriesStart is the start of the default window of the web
// activity view ending at until: 28 days, 16 weeks or 12 months, aligned
// to the period in loc.
func defaultSeriesStart(p store.Period, until time.Time, loc *time.Location) time.Time {
	y, m, d := until.In(loc).Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, loc)
	switch p {
	case store.PeriodWeek:
		monday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
		return monday.AddDate(0, 0, -7*15)
	case store.PeriodMonth:
		return time.Date(y, m, 1, 0, 0, 0, 0, loc).AddDate(0, -11, 0)
	default:
		return day.AddDate(0, 0, -27)
	}
}
