// Package claude implements the harness adapter for the Claude Code JSONL
// transcript format (~/.claude/projects/**/*.jsonl, including subagent
// transcripts under <session-uuid>/subagents/**).
package claude

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/model"
)

// HarnessName is the short name used in session and event IDs.
const HarnessName = "claude"

// DefaultToolResultMaxBytes is the default cap for tool_result event text.
const DefaultToolResultMaxBytes = 4096

// DefaultToolInputMaxBytes is the default cap for tool_use event text.
const DefaultToolInputMaxBytes = 1024

// Adapter reads Claude Code transcripts. It is safe for concurrent use.
type Adapter struct {
	dir           string
	toolResultMax int
	toolInputMax  int
	classifier    Classifier
	skip          map[string]bool
	// fileHistory is the directory of the backup copies of edited files
	// (WithFileHistoryDir); empty leaves them out.
	fileHistory    string
	fileHistoryMax int64
	logf           func(format string, args ...any)

	mu    sync.Mutex
	stats Stats
}

// Stats are cumulative counters over all Parse calls of an Adapter.
type Stats struct {
	Lines     int64            // complete, non-blank lines consumed
	Malformed int64            // lines that were not valid JSON objects (skipped)
	Records   map[string]int64 // lines per record "type"
	// Backups counts the backup files of edited files read for the
	// archive (WithFileHistoryDir), BackupBytes their size;
	// BackupsMissing the ones a record names that were no longer on disk,
	// BackupsOversized the ones over the size cap.
	Backups, BackupBytes, BackupsMissing, BackupsOversized int64
}

// Option configures an Adapter.
type Option func(*Adapter)

// WithToolResultMaxBytes caps the text of tool_result events (default 4096). n <= 0 keeps the default.
func WithToolResultMaxBytes(n int) Option {
	return func(a *Adapter) {
		if n > 0 {
			a.toolResultMax = n
		}
	}
}

// WithToolInputMaxBytes caps the text of tool_use events (default 1024). n <= 0 keeps the default.
func WithToolInputMaxBytes(n int) Option {
	return func(a *Adapter) {
		if n > 0 {
			a.toolInputMax = n
		}
	}
}

// WithPeerPrefixes adds text prefixes that classify a user message as actor "peer".
func WithPeerPrefixes(p ...string) Option {
	return func(a *Adapter) { a.classifier.PeerPrefixes = append(a.classifier.PeerPrefixes, p...) }
}

// WithPeerPasteHeaders adds first-line prefixes that classify a message
// consisting of one pasted block as actor "peer" (Classifier.PeerPasteHeaders).
func WithPeerPasteHeaders(h ...string) Option {
	return func(a *Adapter) { a.classifier.PeerPasteHeaders = append(a.classifier.PeerPasteHeaders, h...) }
}

// WithAutomationPrefixes adds text prefixes that classify a user message as actor "automation".
func WithAutomationPrefixes(p ...string) Option {
	return func(a *Adapter) {
		a.classifier.AutomationPrefixes = append(a.classifier.AutomationPrefixes, p...)
	}
}

// WithSkipRecordTypes skips additional record types (on top of all non-ingested types).
func WithSkipRecordTypes(types ...string) Option {
	return func(a *Adapter) {
		for _, t := range types {
			a.skip[t] = true
		}
	}
}

// New returns an adapter reading transcripts below dir. An empty dir means
// DefaultProjectsDir(); if that cannot be determined, Sources returns an error.
func New(dir string, opts ...Option) *Adapter {
	a := &Adapter{
		dir:           dir,
		toolResultMax: DefaultToolResultMaxBytes,
		toolInputMax:  DefaultToolInputMaxBytes,
		classifier:    DefaultClassifier(),
		skip:          map[string]bool{},
		stats:         Stats{Records: map[string]int64{}},

		fileHistoryMax: DefaultFileHistoryMaxBytes,
		logf:           func(string, ...any) {},
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Register creates an adapter with New and adds it to the harness registry.
func Register(dir string, opts ...Option) *Adapter {
	a := New(dir, opts...)
	harness.Register(a)
	return a
}

// DefaultProjectsDir returns $CLAUDE_CONFIG_DIR/projects, or ~/.claude/projects.
func DefaultProjectsDir() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "projects"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("claude: projects dir: %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// Name implements harness.Adapter.
func (a *Adapter) Name() string { return HarnessName }

// Stats returns a copy of the cumulative parse counters.
func (a *Adapter) Stats() Stats {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.stats
	s.Records = make(map[string]int64, len(a.stats.Records))
	for k, v := range a.stats.Records {
		s.Records[k] = v
	}
	return s
}

// Sources implements harness.Adapter: every *.jsonl below the projects dir,
// as absolute paths in lexical order. A missing projects dir yields no sources.
func (a *Adapter) Sources() ([]string, error) {
	dir := a.dir
	if dir == "" {
		d, err := DefaultProjectsDir()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claude: walk %s: %w", dir, err)
	}
	sort.Strings(out)
	return out, nil
}

// Parse implements harness.Adapter. It reads complete lines from offset on,
// skips malformed JSON lines (counted in Stats), and returns the offset after
// the last consumed line. On an emit error or cancellation it returns the
// offset of the first line that was not fully processed.
func (a *Adapter) Parse(ctx context.Context, path string, offset int64, emit func(model.Record) error) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return offset, err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	p := newParser(a, abs)
	defer p.finishFileHistory()
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		if err := ctx.Err(); err != nil {
			return offset, err
		}
		line, err := r.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return offset, nil // partial trailing line (if any) is left for later
			}
			return offset, err
		}
		next := offset + int64(len(line))
		line = bytes.TrimRight(line, "\r\n")
		if len(bytes.TrimSpace(line)) > 0 {
			rec, typ, ok := p.parseLine(line, offset)
			a.count(typ, ok)
			if rec != nil {
				if err := emit(*rec); err != nil {
					return offset, err
				}
			}
		}
		offset = next
	}
}

func (a *Adapter) count(typ string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stats.Lines++
	if !ok {
		a.stats.Malformed++
		return
	}
	a.stats.Records[typ]++
}
