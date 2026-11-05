// Package logfile is the log of total-recall (config.LogPath): hooks and
// detached commands append timestamped lines to it, status and the TUI
// read the errors back. Writing never fails loudly, because a hook must
// never disturb the harness; the file is rotated once above a size limit,
// one old generation is kept.
package logfile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultMaxSize is the log size above which the file is rotated to
// "<log>.1" (one old generation is kept).
const DefaultMaxSize = 5 << 20

// Logger appends timestamped lines to the log file (config.LogPath). Every
// write opens and closes the file: hook runs are short and may run in
// parallel, and O_APPEND keeps lines intact.
type Logger struct {
	path    string
	now     func() time.Time
	maxSize int64
}

// New returns a logger for the file at path; now is the clock of the
// timestamps.
func New(path string, now func() time.Time) *Logger {
	return &Logger{path: path, now: now, maxSize: DefaultMaxSize}
}

// SetMaxSize changes the size above which the log is rotated (tests).
func (l *Logger) SetMaxSize(n int64) { l.maxSize = n }

// rotate renames the log to "<log>.1" (replacing an older one) once it
// exceeds maxSize. Parallel writers may both rename; at worst a few lines
// end up in the old generation.
func (l *Logger) rotate() {
	if fi, err := os.Stat(l.path); err == nil && fi.Size() > l.maxSize {
		_ = os.Rename(l.path, l.path+".1")
	}
}

// Printf writes one line "<RFC3339 UTC> <LEVEL> <message>". Failures to log
// are ignored: logging must never break a hook.
func (l *Logger) Printf(level, format string, args ...any) {
	msg := strings.TrimRight(fmt.Sprintf(format, args...), "\n")
	msg = strings.ReplaceAll(msg, "\n", " | ")
	line := fmt.Sprintf("%s %s %s\n", l.now().UTC().Format(time.RFC3339), level, msg)
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return
	}
	l.rotate()
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

// Errorf logs at level ERROR.
func (l *Logger) Errorf(format string, args ...any) { l.Printf("ERROR", format, args...) }

// Infof logs at level INFO.
func (l *Logger) Infof(format string, args ...any) { l.Printf("INFO", format, args...) }

// Writer returns an io.Writer that logs every written line at level.
func (l *Logger) Writer(level string) io.Writer { return &writer{l: l, level: level} }

// writer adapts a Logger to io.Writer, one log line per written line.
type writer struct {
	l     *Logger
	level string
}

func (w *writer) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			w.l.Printf(w.level, "%s", line)
		}
	}
	return len(p), nil
}

// Error is one ERROR line of the log file.
type Error struct {
	// At is the line's timestamp; zero when the line has none that parses
	// (e.g. stderr of a detached child).
	At      time.Time
	Message string
}

// ReadErrors returns the ERROR lines of the log file in file order, without
// the lines that only report a missing transcript (see isMissingTranscript).
// A missing file is no error and yields nil.
func ReadErrors(path string) ([]Error, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []Error
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		stamp, rest, ok := strings.Cut(line, " ERROR ")
		if !ok {
			if msg, found := strings.CutPrefix(line, "ERROR "); found {
				stamp, rest, ok = "", msg, true
			}
		}
		if !ok || strings.Contains(stamp, " ") {
			continue
		}
		if isMissingTranscript(rest) {
			continue
		}
		e := Error{Message: rest}
		if t, err := time.Parse(time.RFC3339, stamp); err == nil {
			e.At = t
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// isMissingTranscript reports whether msg is a hook error about a transcript
// file that did not exist. Versions before 0.1.1 logged that case as ERROR;
// since then it is INFO ("transcript not present, skipped"). Harnesses
// announce files they have not written yet, so this is never a failure.
func isMissingTranscript(msg string) bool {
	return strings.HasPrefix(msg, "hook ") && strings.Contains(msg, "no such file or directory")
}

// SplitErrors partitions errs into the lines at or after since (recent, in
// file order) and the count of older ones. Lines without a timestamp are
// recent: an undated error is never hidden.
func SplitErrors(errs []Error, since time.Time) (recent []Error, older int) {
	for _, e := range errs {
		if !e.At.IsZero() && e.At.Before(since) {
			older++
			continue
		}
		recent = append(recent, e)
	}
	return recent, older
}
