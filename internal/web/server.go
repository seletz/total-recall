package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/seletz/total-recall/internal/store"
)

// Options configure a Server. Token, Search, Activity and Status are
// required; Watcher is optional (without it /api/events answers 404).
type Options struct {
	// Token authenticates every request below /api/: the UI sends it as
	// "Authorization: Bearer <token>". The CLI generates a random one per
	// run and prints it in the URL fragment of the start page.
	Token string
	// Version is reported by /api/status.
	Version string
	// Location is the time zone a plain date parameter (YYYY-MM-DD) is
	// read in; nil means UTC.
	Location *time.Location
	// Assets is the UI build; nil uses Dist().
	Assets fs.FS
	// Search answers GET /api/search with the JSON of `search --json`.
	Search func(ctx context.Context, q SearchQuery) (any, error)
	// Activity answers GET /api/activity with the JSON of
	// `stats --period --by --json`.
	Activity func(ctx context.Context, q ActivityQuery) (any, error)
	// Status answers GET /api/status.
	Status func(ctx context.Context) (Status, error)
	// Watcher is polled every PollInterval for changes to the archive; a
	// change is pushed to every client of GET /api/events (SSE). A
	// read-only store implements it.
	Watcher Watcher
	// PollInterval is the interval of the poll; default 2s.
	PollInterval time.Duration
	// Log receives one line per request error and poller problem; nil
	// discards them.
	Log io.Writer
}

// Watcher is the part of store.Store the poller needs.
type Watcher interface {
	Poll(ctx context.Context) (store.Change, error)
	Reopen() error
}

// SearchQuery are the parameters of GET /api/search.
type SearchQuery struct {
	Query string
	// Actor restricts the hits to one actor; empty means all.
	Actor string
	// Raw passes the query to FTS5 unchanged.
	Raw bool
	// Limit caps the hits; 0 keeps the default of the caller.
	Limit int
	// Since and Until bound the event time; zero means unbounded.
	Since, Until time.Time
	// HighlightStart and HighlightEnd wrap the matches in the snippets.
	// The server picks characters that cannot occur in text, so the UI
	// can split a snippet without interpreting HTML.
	HighlightStart, HighlightEnd string
}

// Highlight markers of SearchQuery: private-use code points, never in text.
const (
	HighlightStart = ""
	HighlightEnd   = ""
)

// ActivityQuery are the parameters of GET /api/activity.
type ActivityQuery struct {
	Period string // day, week or month
	By     string // project, agent, directory, account or ""
	Top    int
	// Since and Until bound the range; zero means the caller's default.
	Since, Until time.Time
}

// Status is the JSON of GET /api/status.
type Status struct {
	Version          string            `json:"version"`
	Sessions         int               `json:"sessions"`
	Events           int               `json:"events"`
	Sources          int               `json:"sources"`
	LastIngest       map[string]string `json:"last_ingest"`
	SchemaVersion    int               `json:"schema_version"`
	MigrationPending bool              `json:"migration_pending"`
	Timezone         string            `json:"timezone"`
}

// Server is the HTTP handler of `total-recall serve` plus the poller that
// feeds /api/events.
type Server struct {
	opts   Options
	assets fs.FS
	mux    *http.ServeMux

	mu   sync.Mutex
	subs map[chan string]struct{}
}

// New builds a Server; it validates nothing but the token, which must not
// be empty.
func New(opts Options) (*Server, error) {
	if opts.Token == "" {
		return nil, errors.New("web: empty token")
	}
	if opts.Assets == nil {
		opts.Assets = Dist()
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	s := &Server{opts: opts, assets: opts.Assets, mux: http.NewServeMux(), subs: map[chan string]struct{}{}}
	s.mux.HandleFunc("GET /api/status", s.status)
	s.mux.HandleFunc("GET /api/search", s.search)
	s.mux.HandleFunc("GET /api/activity", s.activity)
	s.mux.HandleFunc("GET /api/events", s.events)
	s.mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such API route")
	}))
	s.mux.Handle("/", s.spa())
	return s, nil
}

// Handler returns the handler with the guards in front: loopback Host,
// loopback Origin, same-origin fetch metadata, the token below /api/, and
// the security headers on every answer.
func (s *Server) Handler() http.Handler {
	return s.guard(s.mux)
}

// Serve runs the poller and serves ln until ctx is done, then shuts the
// server down (open SSE streams end with the context, the rest gets five
// seconds).
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	if s.opts.Watcher != nil {
		go s.runPoller(ctx)
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
	}
	// Serve has returned ErrServerClosed by now; nothing else to report.
	<-errc //nolint:errcheck // the channel carries ErrServerClosed after Shutdown
	return nil
}

// ---- guards ----

// contentSecurityPolicy forbids everything that is not part of the build:
// no external scripts, styles, fonts, images or connections, no framing.
const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// loopback reports whether host (with or without port) names this machine
// by a name that cannot be rebound: localhost or a loopback IP. A request
// whose Host is a public name that resolves to 127.0.0.1 (DNS rebinding)
// is rejected, as is one that arrives through a tunnel under another name.
func loopback(host string) bool {
	h := host
	if hp, _, err := net.SplitHostPort(host); err == nil {
		h = hp
	}
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		api := strings.HasPrefix(r.URL.Path, "/api/")
		if api {
			h.Set("Cache-Control", "no-store")
		}
		if !loopback(r.Host) {
			writeError(w, http.StatusForbidden, "forbidden", "host not allowed")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
			u, err := url.Parse(origin)
			if err != nil || !loopback(u.Host) {
				writeError(w, http.StatusForbidden, "forbidden", "origin not allowed")
				return
			}
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			writeError(w, http.StatusForbidden, "forbidden", "cross-site request")
			return
		}
		if api {
			if !s.authorized(r) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing or wrong token")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// authorized checks the bearer token in constant time.
func (s *Server) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return false
	}
	got, want := []byte(strings.TrimSpace(auth[len(prefix):])), []byte(s.opts.Token)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ---- API ----

// errorJSON mirrors the ErrorJSON of the CLI: {"error":{"kind","message"}}.
type errorJSON struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, kind, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorJSON{Error: errorBody{Kind: kind, Message: msg}})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		// Headers are out; nothing better to do than to drop the stream.
		return
	}
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	_, _ = fmt.Fprintf(s.opts.Log, "%s %s: %v\n", r.Method, r.URL.Path, err)
	writeError(w, http.StatusInternalServerError, "internal", err.Error())
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	st, err := s.opts.Status(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, st)
}

// queryTime parses a time parameter: RFC 3339, or a date (YYYY-MM-DD) in
// loc. Empty is the zero time.
func queryTime(v string, loc *time.Location) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q (use RFC 3339 or YYYY-MM-DD)", v)
}

func queryInt(v string, def int) (int, error) {
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid number %q", v)
	}
	return n, nil
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query()
	q := SearchQuery{Query: strings.TrimSpace(p.Get("q")), Actor: p.Get("actor"), Raw: p.Get("raw") == "1",
		HighlightStart: HighlightStart, HighlightEnd: HighlightEnd}
	if q.Query == "" {
		writeJSON(w, []any{})
		return
	}
	var err error
	if q.Limit, err = queryInt(p.Get("limit"), 100); err != nil {
		writeError(w, http.StatusBadRequest, "usage", "limit: "+err.Error())
		return
	}
	if q.Since, err = queryTime(p.Get("since"), s.opts.Location); err != nil {
		writeError(w, http.StatusBadRequest, "usage", "since: "+err.Error())
		return
	}
	if q.Until, err = queryTime(p.Get("until"), s.opts.Location); err != nil {
		writeError(w, http.StatusBadRequest, "usage", "until: "+err.Error())
		return
	}
	out, err := s.opts.Search(r.Context(), q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query()
	q := ActivityQuery{Period: p.Get("period"), By: p.Get("by")}
	var err error
	if q.Top, err = queryInt(p.Get("top"), 0); err != nil {
		writeError(w, http.StatusBadRequest, "usage", "top: "+err.Error())
		return
	}
	if q.Since, err = queryTime(p.Get("since"), s.opts.Location); err != nil {
		writeError(w, http.StatusBadRequest, "usage", "since: "+err.Error())
		return
	}
	if q.Until, err = queryTime(p.Get("until"), s.opts.Location); err != nil {
		writeError(w, http.StatusBadRequest, "usage", "until: "+err.Error())
		return
	}
	out, err := s.opts.Activity(r.Context(), q)
	if err != nil {
		var ue usageError
		if errors.As(err, &ue) {
			writeError(w, http.StatusBadRequest, "usage", err.Error())
			return
		}
		s.fail(w, r, err)
		return
	}
	writeJSON(w, out)
}

// usageError marks a parameter error of an API function: it is answered
// with 400 instead of 500.
type usageError struct{ error }

// Usage wraps err as a parameter error (HTTP 400).
func Usage(err error) error { return usageError{err} }

// ---- SSE ----

// events streams one "change" event per change of the archive, as
// text/event-stream, until the client goes away or the server stops. A
// comment line every 15 seconds keeps the connection alive.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if s.opts.Watcher == nil {
		writeError(w, http.StatusNotFound, "not_found", "live updates are off")
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	ch := s.subscribe()
	defer s.unsubscribe(ch)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": connected\n\n")
	fl.Flush()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case change := <-ch:
			if _, err := fmt.Fprintf(w, "event: change\ndata: {\"change\":%q}\n\n", change); err != nil {
				return
			}
			fl.Flush()
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func (s *Server) subscribe() chan string {
	ch := make(chan string, 4)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *Server) unsubscribe(ch chan string) {
	s.mu.Lock()
	delete(s.subs, ch)
	s.mu.Unlock()
}

// broadcast hands change to every subscriber; a slow one drops it (the
// next change comes anyway and the UI reloads whole views).
func (s *Server) broadcast(change string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- change:
		default:
		}
	}
}

// runPoller polls the watcher at the interval and broadcasts changes; a
// replaced archive is reopened first. Poll is for one caller at a time, so
// this is the only one.
func (s *Server) runPoller(ctx context.Context) {
	t := time.NewTicker(s.opts.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.pollOnce(ctx)
	}
}

// pollOnce is one step of the poller.
func (s *Server) pollOnce(ctx context.Context) {
	c, err := s.opts.Watcher.Poll(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(s.opts.Log, "poll: %v\n", err)
		return
	}
	switch c {
	case store.Replaced:
		if err := s.opts.Watcher.Reopen(); err != nil {
			_, _ = fmt.Fprintf(s.opts.Log, "reopen: %v\n", err)
			return
		}
		s.broadcast("replaced")
	case store.Modified:
		s.broadcast("modified")
	case store.Unchanged:
	}
}

// ---- static files ----

// placeholder is served instead of the UI when the binary was built
// without web/dist (plain `go build`).
const placeholder = `<!doctype html>
<meta charset="utf-8">
<title>total-recall</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem}code{background:#eee;padding:.1em .3em}</style>
<h1>total-recall: web UI not built</h1>
<p>This binary was built without the web UI. Build it with
<code>mise run web:build</code> and then <code>mise run build</code>;
the JSON API below <code>/api/</code> works either way.</p>
`

// spa serves the UI build: files as they are (immutable assets with a long
// cache), every other GET the index.html so the UI can route by itself,
// and the placeholder when there is no build. /api/ never falls through.
func (s *Server) spa() http.Handler {
	files := http.FileServerFS(s.assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, "usage", "method not allowed")
			return
		}
		if !Built(s.assets) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, placeholder)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if info, err := fs.Stat(s.assets, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		page, err := fs.ReadFile(s.assets, "index.html")
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "index.html missing")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(page)
	})
}
