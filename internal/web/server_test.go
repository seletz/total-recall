package web

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/seletz/total-recall/internal/store"
)

const token = "secret-token"

// fakeWatcher replays a list of poll results, then Unchanged.
type fakeWatcher struct {
	mu       sync.Mutex
	changes  []store.Change
	reopened int
}

func (f *fakeWatcher) Poll(context.Context) (store.Change, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.changes) == 0 {
		return store.Unchanged, nil
	}
	c := f.changes[0]
	f.changes = f.changes[1:]
	return c, nil
}

func (f *fakeWatcher) Reopen() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reopened++
	return nil
}

func built() fstest.MapFS {
	return fstest.MapFS{
		"index.html":       {Data: []byte("<!doctype html><title>ui</title>")},
		"assets/app-1.js":  {Data: []byte("console.log(1)")},
		"assets/app-1.css": {Data: []byte("body{}")},
		"favicon.svg":      {Data: []byte("<svg/>")},
	}
}

// newServer builds a server whose API functions record their arguments.
func newServer(t *testing.T, assets fstest.MapFS, w Watcher) (*Server, *calls) {
	t.Helper()
	c := &calls{}
	s, err := New(Options{
		Token: token, Version: "test", Assets: assets, Watcher: w, PollInterval: time.Hour,
		Search: func(_ context.Context, q SearchQuery) (any, error) {
			c.search = append(c.search, q)
			return []map[string]any{{"snippet": "a " + q.HighlightStart + "hit" + q.HighlightEnd}}, nil
		},
		Activity: func(_ context.Context, q ActivityQuery) (any, error) {
			c.activity = append(c.activity, q)
			if q.Period == "bogus" {
				return nil, Usage(errors.New("unknown period"))
			}
			if q.Period == "boom" {
				return nil, errors.New("database on fire")
			}
			return map[string]any{"period": q.Period}, nil
		},
		Status: func(context.Context) (Status, error) {
			return Status{Version: "test", Sessions: 3, Events: 30, LastIngest: map[string]string{}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

type calls struct {
	search   []SearchQuery
	activity []ActivityQuery
}

// get performs a request against h with the given host and headers.
func get(h http.Handler, host, target string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Host = host
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func bearer(extra ...string) map[string]string {
	h := map[string]string{"Authorization": "Bearer " + token}
	for i := 0; i+1 < len(extra); i += 2 {
		h[extra[i]] = extra[i+1]
	}
	return h
}

func TestNewNeedsToken(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("New without token: no error")
	}
}

func TestLoopback(t *testing.T) {
	for _, tc := range []struct {
		host string
		want bool
	}{
		{"127.0.0.1:8787", true}, {"127.0.0.1", true}, {"localhost:8787", true}, {"LOCALHOST", true},
		{"[::1]:8787", true}, {"::1", true}, {"127.0.0.2:1", true},
		{"evil.example.com:8787", false}, {"192.168.1.5:8787", false}, {"", false}, {"localhost.evil.com", false},
	} {
		if got := loopback(tc.host); got != tc.want {
			t.Errorf("loopback(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestGuard(t *testing.T) {
	s, _ := newServer(t, built(), nil)
	h := s.Handler()
	tests := []struct {
		name   string
		host   string
		target string
		hdr    map[string]string
		code   int
		body   string
	}{
		{"page from loopback", "127.0.0.1:8787", "/", nil, 200, "<title>ui"},
		{"page from localhost", "localhost:8787", "/", nil, 200, "<title>ui"},
		{"page from rebound name", "evil.example.com", "/", nil, 403, "host not allowed"},
		{"api without token", "127.0.0.1:8787", "/api/status", nil, 401, "token"},
		{"api with wrong token", "127.0.0.1:8787", "/api/status", map[string]string{"Authorization": "Bearer nope"}, 401, "token"},
		{"api with token", "127.0.0.1:8787", "/api/status", bearer(), 200, `"sessions":3`},
		{"api with lowercase scheme", "127.0.0.1:8787", "/api/status", map[string]string{"Authorization": "bearer " + token}, 200, `"sessions":3`},
		{"api from foreign origin", "127.0.0.1:8787", "/api/status", bearer("Origin", "http://evil.example.com"), 403, "origin"},
		{"api from dev server origin", "127.0.0.1:8787", "/api/status", bearer("Origin", "http://localhost:5173"), 200, `"sessions"`},
		{"api from null origin", "127.0.0.1:8787", "/api/status", bearer("Origin", "null"), 200, `"sessions"`},
		{"api cross-site", "127.0.0.1:8787", "/api/status", bearer("Sec-Fetch-Site", "cross-site"), 403, "cross-site"},
		{"api same-origin", "127.0.0.1:8787", "/api/status", bearer("Sec-Fetch-Site", "same-origin"), 200, `"sessions"`},
		{"api typed in by hand", "127.0.0.1:8787", "/api/status", bearer("Sec-Fetch-Site", "none"), 200, `"sessions"`},
		{"unknown api route", "127.0.0.1:8787", "/api/nope", bearer(), 404, "no such API route"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := get(h, tc.host, tc.target, tc.hdr)
			if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.body) {
				t.Fatalf("got %d %q, want %d containing %q", w.Code, w.Body.String(), tc.code, tc.body)
			}
			if got := w.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'none'") {
				t.Errorf("CSP = %q", got)
			}
			if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("nosniff = %q", got)
			}
			if strings.HasPrefix(tc.target, "/api/") && w.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("api Cache-Control = %q", w.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestSearch(t *testing.T) {
	s, c := newServer(t, built(), nil)
	h := s.Handler()
	tests := []struct {
		name   string
		target string
		code   int
		body   string
		want   *SearchQuery
	}{
		{"empty query", "/api/search", 200, "[]", nil},
		{"blank query", "/api/search?q=%20", 200, "[]", nil},
		{"bad limit", "/api/search?q=x&limit=-1", 400, "limit", nil},
		{"bad since", "/api/search?q=x&since=yesterday", 400, "since", nil},
		{"defaults", "/api/search?q=release", 200, "hit",
			&SearchQuery{Query: "release", Limit: 100, HighlightStart: HighlightStart, HighlightEnd: HighlightEnd}},
		{"all params", "/api/search?q=a+b&actor=human&raw=1&limit=5&since=2026-10-01&until=2026-10-02T10:00:00Z", 200, "hit",
			&SearchQuery{Query: "a b", Actor: "human", Raw: true, Limit: 5,
				Since: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Until: time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC),
				HighlightStart: HighlightStart, HighlightEnd: HighlightEnd}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c.search = nil
			w := get(h, "127.0.0.1:1", tc.target, bearer())
			if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.body) {
				t.Fatalf("got %d %q, want %d containing %q", w.Code, w.Body.String(), tc.code, tc.body)
			}
			if tc.want == nil {
				if len(c.search) != 0 {
					t.Fatalf("search called with %+v", c.search)
				}
				return
			}
			if len(c.search) != 1 || !c.search[0].Since.Equal(tc.want.Since) || !c.search[0].Until.Equal(tc.want.Until) {
				t.Fatalf("search calls = %+v, want %+v", c.search, *tc.want)
			}
			got, want := c.search[0], *tc.want
			got.Since, got.Until, want.Since, want.Until = time.Time{}, time.Time{}, time.Time{}, time.Time{}
			if got != want {
				t.Fatalf("search query = %+v, want %+v", got, want)
			}
		})
	}
}

func TestActivity(t *testing.T) {
	s, c := newServer(t, built(), nil)
	h := s.Handler()
	tests := []struct {
		name   string
		target string
		code   int
		body   string
		want   *ActivityQuery
	}{
		{"usage error", "/api/activity?period=bogus", 400, "unknown period", nil},
		{"internal error", "/api/activity?period=boom", 500, "database on fire", nil},
		{"bad top", "/api/activity?top=x", 400, "top", nil},
		{"params", "/api/activity?period=week&by=project&top=6&since=2026-09-01&until=2026-10-01", 200, `"period":"week"`,
			&ActivityQuery{Period: "week", By: "project", Top: 6,
				Since: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Until: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c.activity = nil
			w := get(h, "127.0.0.1:1", tc.target, bearer())
			if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.body) {
				t.Fatalf("got %d %q, want %d containing %q", w.Code, w.Body.String(), tc.code, tc.body)
			}
			if tc.want != nil {
				if len(c.activity) != 1 || c.activity[0].Period != tc.want.Period || c.activity[0].By != tc.want.By ||
					c.activity[0].Top != tc.want.Top || !c.activity[0].Since.Equal(tc.want.Since) || !c.activity[0].Until.Equal(tc.want.Until) {
					t.Fatalf("activity calls = %+v, want %+v", c.activity, *tc.want)
				}
			}
		})
	}
}

func TestSPA(t *testing.T) {
	s, _ := newServer(t, built(), nil)
	h := s.Handler()
	tests := []struct {
		name   string
		method string
		target string
		code   int
		body   string
		cache  string
	}{
		{"index", http.MethodGet, "/", 200, "<title>ui", "no-cache"},
		{"explicit index", http.MethodGet, "/index.html", 200, "<title>ui", "no-cache"},
		{"client route falls back", http.MethodGet, "/stats", 200, "<title>ui", "no-cache"},
		{"nested client route", http.MethodGet, "/session/claude:abc", 200, "<title>ui", "no-cache"},
		{"hashed asset", http.MethodGet, "/assets/app-1.js", 200, "console.log", "public, max-age=31536000, immutable"},
		{"plain file", http.MethodGet, "/favicon.svg", 200, "<svg", ""},
		{"directory", http.MethodGet, "/assets/", 200, "<title>ui", "no-cache"},
		{"traversal is cleaned by the mux", http.MethodGet, "/../index.html", 307, "/index.html", ""},
		{"post", http.MethodPost, "/", 405, "method not allowed", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.target, nil)
			r.Host = "127.0.0.1:1"
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.body) {
				t.Fatalf("got %d %q, want %d containing %q", w.Code, w.Body.String(), tc.code, tc.body)
			}
			if got := w.Header().Get("Cache-Control"); got != tc.cache {
				t.Errorf("Cache-Control = %q, want %q", got, tc.cache)
			}
		})
	}
}

func TestPlaceholderWithoutBuild(t *testing.T) {
	s, _ := newServer(t, fstest.MapFS{".gitkeep": {}}, nil)
	h := s.Handler()
	w := get(h, "127.0.0.1:1", "/", nil)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "web UI not built") {
		t.Fatalf("placeholder: got %d %q", w.Code, w.Body.String())
	}
	if w := get(h, "127.0.0.1:1", "/api/status", bearer()); w.Code != 200 {
		t.Fatalf("api without build: %d %s", w.Code, w.Body.String())
	}
	if Built(Dist()) {
		t.Log("embedded dist holds a build")
	}
}

func TestEventsOffWithoutWatcher(t *testing.T) {
	s, _ := newServer(t, built(), nil)
	if w := get(s.Handler(), "127.0.0.1:1", "/api/events", bearer()); w.Code != 404 {
		t.Fatalf("events without watcher: %d %s", w.Code, w.Body.String())
	}
}

func TestEvents(t *testing.T) {
	fw := &fakeWatcher{changes: []store.Change{store.Unchanged, store.Modified, store.Replaced}}
	s, _ := newServer(t, built(), fw)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
	req.Host = "127.0.0.1:1"
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	rd := bufio.NewReader(resp.Body)
	if line, _ := rd.ReadString('\n'); line != ": connected\n" {
		t.Fatalf("first line = %q", line)
	}
	// Wait until the stream is subscribed, then drive the poller by hand.
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		n := len(s.subs)
		s.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for range 3 {
		s.pollOnce(ctx)
	}
	var got []string
	for len(got) < 2 {
		line, err := rd.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v (got %q)", err, got)
		}
		if strings.HasPrefix(line, "data: ") {
			got = append(got, strings.TrimSpace(line))
		}
	}
	if got[0] != `data: {"change":"modified"}` || got[1] != `data: {"change":"replaced"}` {
		t.Fatalf("events = %q", got)
	}
	if fw.reopened != 1 {
		t.Fatalf("reopened = %d, want 1", fw.reopened)
	}
}

func TestServe(t *testing.T) {
	s, _ := newServer(t, built(), &fakeWatcher{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	req, _ := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"version":"test"`) {
		t.Fatalf("status: %d %s", resp.StatusCode, body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not stop")
	}
}
