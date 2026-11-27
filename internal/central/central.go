// Package central delivers local archives of a pool of hosts to one
// central archive (spike #347): a host pushes what is new in its local
// archive (store.Export) over HTTP(S) to the receiver, which writes it into
// the central archive (store.Import) under the host name its token maps
// to. The local archive is the buffer: when the receiver is unreachable
// nothing is lost, the next push delivers it, and the hooks never wait for
// the network.
//
// Protocol v1: POST /v1/push with "Authorization: Bearer <token>", body a
// zstd-compressed JSON store.Changeset; the answer is the JSON
// store.ImportResult. GET /v1/health answers "ok". The sender advances its
// state only after a 200, a batch delivered twice changes nothing.
package central

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/seletz/total-recall/internal/store"
)

// PushPath is the endpoint of a delivery.
const PushPath = "/v1/push"

// HashToken returns the hex SHA-256 of a host token; the receiver keeps
// only hashes.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ReadHosts reads a hosts file: one "<host> <sha256 of its token>" per line,
// "#" starts a comment.
func ReadHosts(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	hosts := map[string]string{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		fs := strings.Fields(line)
		if len(fs) != 2 || len(fs[1]) != 64 {
			return nil, fmt.Errorf("%s:%d: want \"<host> <sha256 hex of the token>\"", path, n)
		}
		hosts[strings.ToLower(fs[1])] = fs[0]
	}
	return hosts, sc.Err()
}

// Server receives deliveries into a central archive.
type Server struct {
	Store *store.Store
	// Hosts maps the SHA-256 of a token to the host name it stands for.
	// The host name comes from the token, never from the request.
	Hosts map[string]string
	// MaxBody limits a compressed request body (default 64 MiB).
	MaxBody int64
	// Logf receives one line per delivery and per refusal.
	Logf func(format string, args ...any)
	mu   sync.Mutex // SQLite has one writer; deliveries are serialized
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// host returns the host the request's token belongs to.
func (s *Server) host(r *http.Request) (string, bool) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" {
		return "", false
	}
	want := HashToken(tok)
	for h, host := range s.Hosts {
		if subtle.ConstantTimeCompare([]byte(h), []byte(want)) == 1 {
			return host, true
		}
	}
	return "", false
}

// Handler serves the protocol.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok\n") })
	mux.HandleFunc("POST "+PushPath, s.push)
	return mux
}

func (s *Server) push(w http.ResponseWriter, r *http.Request) {
	host, ok := s.host(r)
	if !ok {
		s.logf("receive: refused %s: unknown token", r.RemoteAddr)
		http.Error(w, "unknown token", http.StatusUnauthorized)
		return
	}
	max := s.MaxBody
	if max <= 0 {
		max = 64 << 20
	}
	dec, err := zstd.NewReader(http.MaxBytesReader(w, r.Body, max), zstd.WithDecoderMaxMemory(uint64(8*max)))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dec.Close()
	var cs store.Changeset
	if err := json.NewDecoder(dec).Decode(&cs); err != nil {
		s.logf("receive: %s: bad body: %v", host, err)
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	res, err := s.Store.Import(r.Context(), host, cs)
	s.mu.Unlock()
	if err != nil {
		s.logf("receive: %s: %v", host, err)
		code := http.StatusInternalServerError
		if cs.Schema > store.LatestSchemaVersion() {
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	s.logf("receive: %s: %d sessions, %d events, %d meta, %d blobs new, %d conflicts", host, res.Sessions, res.Events, res.Meta, res.Blobs, res.Conflicts)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// Client delivers changesets to a receiver.
type Client struct {
	URL   string // base URL, e.g. https://archive.example:8787
	Token string
	HTTP  *http.Client
}

// Send delivers one changeset and returns what the receiver wrote.
func (c *Client) Send(ctx context.Context, cs store.Changeset) (store.ImportResult, error) {
	var res store.ImportResult
	var body bytes.Buffer
	enc, err := zstd.NewWriter(&body, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return res, err
	}
	if err := json.NewEncoder(enc).Encode(cs); err != nil {
		return res, err
	}
	if err := enc.Close(); err != nil {
		return res, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+PushPath, &body)
	if err != nil {
		return res, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "zstd")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return res, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return res, fmt.Errorf("central: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return res, json.NewDecoder(resp.Body).Decode(&res)
}

// PushSummary is what Push delivered.
type PushSummary struct {
	Batches  int
	Received store.ImportResult
}

// StatePath is where a host keeps its delivery state for one receiver:
// next to the database, one file per archive and receiver URL (the state
// is the position in this archive; two archives in one directory must not
// share it).
func StatePath(db, url string) string {
	h := sha256.Sum256([]byte(filepath.Base(db) + "\x00" + url))
	return filepath.Join(filepath.Dir(db), "push-"+hex.EncodeToString(h[:6])+".json")
}

// LoadState reads a delivery state; a missing file is the empty state.
func LoadState(path string) (store.SyncState, error) {
	var st store.SyncState
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(b, &st)
}

// SaveState writes a delivery state atomically.
func SaveState(path string, st store.SyncState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Push delivers everything new in st since the state at statePath in
// batches of at most batch events and meta records, saving the state after
// each acknowledged batch. A failure leaves the state at the last
// acknowledged batch; the next Push resumes there.
func Push(ctx context.Context, st *store.Store, c *Client, statePath string, batch int) (PushSummary, error) {
	var sum PushSummary
	state, err := LoadState(statePath)
	if err != nil {
		return sum, fmt.Errorf("central: state %s: %w", statePath, err)
	}
	for {
		cs, err := st.Export(ctx, &state, batch)
		if err != nil {
			return sum, err
		}
		if cs.Empty() && sum.Batches > 0 {
			return sum, nil
		}
		// With nothing new, the empty changeset still goes out once: it
		// checks the token and tells the receiver the host is alive.
		res, err := c.Send(ctx, cs)
		if err != nil {
			return sum, err
		}
		sum.Batches++
		sum.Received.Add(res)
		if cs.Empty() {
			return sum, nil
		}
		state.Commit(cs)
		if err := SaveState(statePath, state); err != nil {
			return sum, fmt.Errorf("central: state %s: %w", statePath, err)
		}
	}
}
