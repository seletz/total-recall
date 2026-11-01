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

// logger appends timestamped lines to the log file (config.LogPath). Every
// write opens and closes the file: hook runs are short and may run in
// parallel, and O_APPEND keeps lines intact.
type logger struct {
	path string
	now  func() time.Time
}

func newLogger(path string, now func() time.Time) *logger { return &logger{path: path, now: now} }

// Printf writes one line "<RFC3339 UTC> <LEVEL> <message>". Failures to log
// are ignored: logging must never break a hook.
func (l *logger) Printf(level, format string, args ...any) {
	msg := strings.TrimRight(fmt.Sprintf(format, args...), "\n")
	msg = strings.ReplaceAll(msg, "\n", " | ")
	line := fmt.Sprintf("%s %s %s\n", l.now().UTC().Format(time.RFC3339), level, msg)
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return
	}
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

// lastErrors returns up to n of the newest ERROR lines of the log file.
func lastErrors(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var ring []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if strings.Contains(sc.Text(), " ERROR ") {
			ring = append(ring, sc.Text())
			if len(ring) > n {
				ring = ring[1:]
			}
		}
	}
	return ring, sc.Err()
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
