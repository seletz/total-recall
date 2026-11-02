package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// defaultLogMaxSize is the log size above which the file is rotated to
// "<log>.1" (one old generation is kept).
const defaultLogMaxSize = 5 << 20

// logger appends timestamped lines to the log file (config.LogPath). Every
// write opens and closes the file: hook runs are short and may run in
// parallel, and O_APPEND keeps lines intact.
type logger struct {
	path    string
	now     func() time.Time
	maxSize int64
}

func newLogger(path string, now func() time.Time) *logger {
	return &logger{path: path, now: now, maxSize: defaultLogMaxSize}
}

// rotate renames the log to "<log>.1" (replacing an older one) once it
// exceeds maxSize. Parallel writers may both rename; at worst a few lines
// end up in the old generation.
func (l *logger) rotate() {
	if fi, err := os.Stat(l.path); err == nil && fi.Size() > l.maxSize {
		_ = os.Rename(l.path, l.path+".1")
	}
}

// Printf writes one line "<RFC3339 UTC> <LEVEL> <message>". Failures to log
// are ignored: logging must never break a hook.
func (l *logger) Printf(level, format string, args ...any) {
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
func (l *logger) Errorf(format string, args ...any) { l.Printf("ERROR", format, args...) }

// Infof logs at level INFO.
func (l *logger) Infof(format string, args ...any) { l.Printf("INFO", format, args...) }

// logWriter adapts a logger to io.Writer, one log line per written line.
type logWriter struct {
	l     *logger
	level string
}

func (w *logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			w.l.Printf(w.level, "%s", line)
		}
	}
	return len(p), nil
}

// logError is one ERROR line of the log file.
type logError struct {
	// At is the line's timestamp; zero when the line has none that parses
	// (e.g. stderr of a detached child).
	At      time.Time
	Message string
}

// readErrors returns the ERROR lines of the log file in file order, without
// the lines that only report a missing transcript (see isMissingTranscript).
// A missing file is no error and yields nil.
func readErrors(path string) ([]logError, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []logError
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
		e := logError{Message: rest}
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

// splitErrors partitions errs into the lines at or after since (recent, in
// file order) and the count of older ones. Lines without a timestamp are
// recent: an undated error is never hidden.
func splitErrors(errs []logError, since time.Time) (recent []logError, older int) {
	for _, e := range errs {
		if !e.At.IsZero() && e.At.Before(since) {
			older++
			continue
		}
		recent = append(recent, e)
	}
	return recent, older
}

// startDetached re-executes the running binary with args in a new session
// (setsid), stdin from /dev/null and stdout/stderr appended to logPath. It
// does not wait for the child.
func startDetached(args []string, logPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	lf, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = lf.Close() }()
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
