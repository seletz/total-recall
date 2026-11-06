package claude

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/seletz/total-recall/internal/harness"
)

// Project memory (#192): Claude Code keeps a memory per project as
// Markdown files in <projects_dir>/<project>/memory/ (an index MEMORY.md
// and one file per memory). The project directory's name is the working
// directory with every character other than a letter or digit replaced by
// "-" (ProjectKey); it is the native project key the store keeps.

// memoryDir is the name of the memory directory inside a project directory.
const memoryDir = "memory"

// cwdProbeBytes bounds how much of a transcript MemoryCwd reads to find
// the working directory; cwdProbeFiles how many transcripts it tries.
const (
	cwdProbeBytes = 1 << 20
	cwdProbeFiles = 3
)

// ProjectKey returns the name of the project directory Claude Code uses
// for the working directory cwd: every rune that is not an ASCII letter or
// digit becomes "-".
func ProjectKey(cwd string) string {
	var b strings.Builder
	for _, r := range cwd {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	return b.String()
}

// root returns the absolute projects dir.
func (a *Adapter) root() (string, error) {
	dir := a.dir
	if dir == "" {
		d, err := DefaultProjectsDir()
		if err != nil {
			return "", err
		}
		dir = d
	}
	return filepath.Abs(dir)
}

// Memory implements harness.MemorySource: every project directory with a
// memory directory, by key, and in it every regular *.md file (sub
// directories included, hidden files and symbolic links skipped), by
// path. ok is false when the projects dir does not exist.
func (a *Adapter) Memory() ([]harness.MemoryProject, bool, error) {
	dir, err := a.root()
	if err != nil {
		return nil, false, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("claude: memory: %w", err)
	}
	var out []harness.MemoryProject
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mdir := filepath.Join(dir, e.Name(), memoryDir)
		if fi, err := os.Stat(mdir); err != nil || !fi.IsDir() {
			continue
		}
		files, err := memoryFiles(mdir)
		if err != nil {
			return nil, true, err
		}
		out = append(out, harness.MemoryProject{Key: e.Name(), Files: files})
	}
	return out, true, nil
}

// memoryFiles lists the memory files below mdir.
func memoryFiles(mdir string) ([]harness.MemoryFile, error) {
	files := []harness.MemoryFile{}
	err := filepath.WalkDir(mdir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && p != mdir {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(mdir, p)
		if err != nil {
			return err
		}
		files = append(files, harness.MemoryFile{Path: filepath.ToSlash(rel), Abs: p, Size: fi.Size(), MTime: fi.ModTime().UTC()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claude: memory %s: %w", mdir, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// MemoryCwd implements harness.MemorySource: the cwd recorded in a
// transcript of the project directory (the first one whose key matches,
// else the first one found: Claude Code shortens very long names), else
// the first candidate whose ProjectKey is key.
func (a *Adapter) MemoryCwd(key string, candidates []string) string {
	if dir, err := a.root(); err == nil {
		matches, _ := filepath.Glob(filepath.Join(dir, key, "*.jsonl"))
		sort.Strings(matches)
		first := ""
		for i, m := range matches {
			if i >= cwdProbeFiles {
				break
			}
			cwd := transcriptCwd(m)
			if cwd != "" && ProjectKey(cwd) == key {
				return cwd
			}
			if first == "" {
				first = cwd
			}
		}
		if first != "" {
			return first
		}
	}
	for _, c := range candidates {
		if c != "" && ProjectKey(c) == key {
			return c
		}
	}
	return ""
}

// transcriptCwd returns the first cwd field in the first cwdProbeBytes of
// a transcript, "" when there is none.
func transcriptCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(io.LimitReader(f, cwdProbeBytes))
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var rec struct {
				Cwd string `json:"cwd"`
			}
			if json.Unmarshal(line, &rec) == nil && rec.Cwd != "" {
				return rec.Cwd
			}
		}
		if err != nil {
			return ""
		}
	}
}
