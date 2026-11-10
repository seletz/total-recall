package attview

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

// External opens an attachment with the program of the system.
type External interface {
	// Open writes data to a file called name and opens it; it returns the
	// path of the file.
	Open(name string, data []byte) (string, error)
}

// SystemViewer is the External of the real system: the data goes to a
// file in a private temporary directory (one per process, mode 0700, the
// file mode 0600) and the file is opened with `open` (macOS), `xdg-open`
// (Linux and the BSDs) or `start` (Windows). The program is started, not
// waited for. The directory stays: the viewer may still read the file
// after the TUI ends; the system cleans its temporary directory.
type SystemViewer struct {
	once sync.Once
	dir  string
	err  error
	// Command builds the command that opens path; nil means the one of
	// the platform. Tests set it.
	Command func(path string) *exec.Cmd
}

// Open implements External.
func (s *SystemViewer) Open(name string, data []byte) (string, error) {
	s.once.Do(func() { s.dir, s.err = os.MkdirTemp("", "total-recall-") })
	if s.err != nil {
		return "", s.err
	}
	name = filepath.Base(name) // no path from the caller leaves the directory
	if name == "." || name == string(filepath.Separator) {
		return "", errors.New("no file name")
	}
	path := filepath.Join(s.dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	cmd := s.command(path)
	if cmd == nil {
		return path, fmt.Errorf("no program to open files on %s", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return path, err
	}
	go func() { _ = cmd.Wait() }() // reap the child
	return path, nil
}

func (s *SystemViewer) command(path string) *exec.Cmd {
	if s.Command != nil {
		return s.Command(path)
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path)
	case "windows":
		return exec.Command("cmd", "/c", "start", "", path)
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly":
		return exec.Command("xdg-open", path)
	}
	return nil
}
