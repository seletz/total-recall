package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/store"
)

func TestServeHelpAndErrors(t *testing.T) {
	e := newTestEnv(t)
	if out := e.mustRun(t, "serve", "--help"); !strings.Contains(out, "--port") || !strings.Contains(out, "--token") || !strings.Contains(out, "--poll") {
		t.Errorf("serve --help = %q", out)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"bad port", []string{"serve", "--port", "70000"}, "port"},
		{"bad poll", []string{"serve", "--poll", "10ms"}, "poll"},
		{"args", []string{"serve", "x"}, "unknown command"},
		{"no archive", []string{"serve", "--port", "0"}, "no archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := e.run(t, "", tc.args...)
			if r.code == 0 || !strings.Contains(r.stderr, tc.want) {
				t.Fatalf("exit %d, stderr %q, want %q", r.code, r.stderr, tc.want)
			}
		})
	}
}

// freePort reserves a loopback port and releases it for the command.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// TestServe runs serve in-process against the small fixture, reads the
// three API routes with the token and stops it through the context.
func TestServe(t *testing.T) {
	e := newTestEnv(t)
	e.mustRun(t, "ingest", "--all")
	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan result, 1)
	go func() {
		done <- e.runCtx(t, ctx, "", "serve", "--port", fmt.Sprint(port), "--token", "t0ken", "--poll", "1s")
	}()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	get := func(path string, token string) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if code, _ := get("/api/status", "t0ken"); code == 200 {
			break
		}
		select {
		case r := <-done:
			t.Fatalf("serve exited early: %d %s %s", r.code, r.stdout, r.stderr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("serve did not come up")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if code, body := get("/api/status", ""); code != 401 {
		t.Errorf("status without token: %d %s", code, body)
	}
	code, body := get("/api/status", "t0ken")
	var st struct {
		Sessions int    `json:"sessions"`
		Events   int    `json:"events"`
		Timezone string `json:"timezone"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil || code != 200 || st.Sessions == 0 || st.Events == 0 || st.Timezone == "" {
		t.Errorf("status: %d %s (%v)", code, body, err)
	}
	code, body = get("/api/search?q=goreleaser", "t0ken")
	var hits []HitJSON
	if err := json.Unmarshal([]byte(body), &hits); err != nil || code != 200 {
		t.Errorf("search: %d %s (%v)", code, body, err)
	}
	for _, h := range hits {
		if !strings.Contains(string(h.Snippet), "") {
			t.Errorf("snippet without highlight markers: %q", h.Snippet)
		}
	}
	code, body = get("/api/activity?period=week&by=project&since=2026-10-01&until=2026-10-31", "t0ken")
	var series ActivitySeriesJSON
	if err := json.Unmarshal([]byte(body), &series); err != nil || code != 200 || series.Period != "week" || series.By != "project" || len(series.Buckets) == 0 {
		t.Errorf("activity: %d %s (%v)", code, body, err)
	}
	if code, body := get("/api/activity?period=year", "t0ken"); code != 400 {
		t.Errorf("activity with bad period: %d %s", code, body)
	}
	code, body = get("/", "")
	if code != 200 && code != 503 {
		t.Errorf("page: %d %s", code, body)
	}
	cancel()
	select {
	case r := <-done:
		if r.code != 0 || !strings.Contains(r.stdout, base+"/#token=t0ken") {
			t.Fatalf("serve: exit %d\nstdout: %s\nstderr: %s", r.code, r.stdout, r.stderr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
}

func TestDefaultSeriesStart(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	until := time.Date(2026, 11, 5, 18, 0, 0, 0, berlin) // a Thursday
	for _, tc := range []struct {
		p    store.Period
		want time.Time
	}{
		{store.PeriodDay, time.Date(2026, 10, 9, 0, 0, 0, 0, berlin)},
		{store.PeriodWeek, time.Date(2026, 7, 20, 0, 0, 0, 0, berlin)},
		{store.PeriodMonth, time.Date(2025, 12, 1, 0, 0, 0, 0, berlin)},
	} {
		if got := defaultSeriesStart(tc.p, until, berlin); !got.Equal(tc.want) {
			t.Errorf("%s: got %s, want %s", tc.p, got, tc.want)
		}
	}
}
