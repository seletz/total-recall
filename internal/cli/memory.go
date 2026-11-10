package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/store"
)

// MemoryFileJSON is one file of `memory --json`: the project (display
// name as in stats --by project, the harness's native key, cwd and git
// root, null while unknown), the path relative to the memory directory
// and the version valid at --at (now by default). SHA256 is null for a
// deleted file (--deleted); `blob SHA256` prints the content.
type MemoryFileJSON struct {
	Project    string     `json:"project"`
	ProjectKey string     `json:"project_key"`
	Harness    string     `json:"harness"`
	Cwd        NullString `json:"cwd"`
	GitRoot    NullString `json:"git_root"`
	Path       string     `json:"path"`
	SHA256     NullString `json:"sha256"`
	Bytes      int64      `json:"bytes"`
	Deleted    bool       `json:"deleted"`
	Changed    string     `json:"changed"`
	Versions   int        `json:"versions"`
}

// MemoryVersionJSON is one version of `memory --history FILE --json`,
// oldest first. TS is the file's mtime for a content, the scan for a
// deletion; ObservedAt the scan that recorded it. SHA256 is null for a
// deletion.
type MemoryVersionJSON struct {
	Project    string     `json:"project"`
	ProjectKey string     `json:"project_key"`
	Harness    string     `json:"harness"`
	Path       string     `json:"path"`
	TS         string     `json:"ts"`
	ObservedAt string     `json:"observed_at"`
	SHA256     NullString `json:"sha256"`
	Bytes      int64      `json:"bytes"`
	Deleted    bool       `json:"deleted"`
}

// MemoryHitJSON is one hit of `memory --grep WORDS --json`: a file whose
// current content matches, best first, with a snippet; Changed is the
// time of the current version.
type MemoryHitJSON struct {
	Project    string     `json:"project"`
	ProjectKey string     `json:"project_key"`
	Harness    string     `json:"harness"`
	Path       string     `json:"path"`
	SHA256     NullString `json:"sha256"`
	Changed    string     `json:"changed"`
	Snippet    NullString `json:"snippet"`
}

// MemoryUseJSON is a memory file that had a blob as content (blob --json).
type MemoryUseJSON struct {
	Project    string `json:"project"`
	ProjectKey string `json:"project_key"`
	Harness    string `json:"harness"`
	Path       string `json:"path"`
	TS         string `json:"ts"`
}

// memProject is a memory project with its display name.
type memProject struct {
	store.MemoryProject
	Name string
}

// memoryProjects loads the memory projects and names them like
// stats --by project: the configured definition, else the name derived
// from the git root or cwd; a project whose cwd is unknown keeps the
// harness's native key.
func memoryProjects(ctx context.Context, st *store.Store, cfg *config.Config) ([]memProject, error) {
	ps, err := st.MemoryProjects(ctx)
	if err != nil {
		return nil, err
	}
	res, err := cfg.ProjectResolver()
	if err != nil {
		return nil, errKind(ErrKindConfig, err)
	}
	raw := make([]string, len(ps))
	var keys, dirs []string
	for i, p := range ps {
		key, dir := p.GitRoot, false
		if key == "" {
			key, dir = p.Cwd, true
		}
		if key == "" {
			continue
		}
		raw[i] = res.Key(key, dir, p.GitRemoteURL)
		keys = append(keys, raw[i])
		if dir {
			dirs = append(dirs, key)
		}
	}
	names := res.Names(keys, dirs, cfg.Home)
	out := make([]memProject, len(ps))
	for i, p := range ps {
		out[i] = memProject{MemoryProject: p, Name: p.Key}
		if raw[i] != "" {
			out[i].Name = names[raw[i]]
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// selectProjects returns the projects --project names: exact matches of
// the name, native key, cwd or git root first, else every project with
// ref in one of them or in the remote URL. Empty ref selects all.
func selectProjects(ps []memProject, ref string) ([]memProject, error) {
	if ref == "" {
		return ps, nil
	}
	var exact, part []memProject
	for _, p := range ps {
		fields := []string{p.Name, p.Key, p.Cwd, p.GitRoot}
		for _, f := range fields {
			if f != "" && f == ref {
				exact = append(exact, p)
				break
			}
		}
		for _, f := range append(fields, p.GitRemoteURL) {
			if f != "" && strings.Contains(f, ref) {
				part = append(part, p)
				break
			}
		}
	}
	if len(exact) > 0 {
		return exact, nil
	}
	if len(part) == 0 {
		return nil, errKind(ErrKindNotFound, fmt.Errorf("no project with memory matches %q", ref))
	}
	return part, nil
}

func projectIDs(ps []memProject) []int64 {
	ids := make([]int64, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	return ids
}

func (e *env) memoryCmd() *cobra.Command {
	var of outputFlags
	var projectRef, at, history, grep string
	var deleted, raw bool
	var limit int
	c := &cobra.Command{
		Use:   "memory",
		Short: "Show the archived project memory of the harness: files, versions, text search",
		Long: `Show the project memory the harness keeps outside the transcripts (Claude
Code: the memory/*.md files of every project), as ingest archived it:
versioned, redacted, deletions included.

  memory                       the memory files of every project as they are now
  memory --at DATE             ... as they were at DATE (local time; a date
                               alone means the end of that day)
  memory --deleted             include deleted files
  memory --history FILE        the versions of FILE (path relative to the
                               memory directory, e.g. MEMORY.md)
  memory --grep WORDS          full-text search over the current content

--project narrows to the projects whose name, path or harness key matches
(exactly, else as a part). The content of a version: blob SHA256.`,
		Args: usageArgs(cobra.NoArgs),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		if history != "" && grep != "" {
			return usagef("use either --history or --grep")
		}
		if grep != "" && (at != "" || deleted) {
			return usagef("--grep searches the current content; it does not combine with --at or --deleted")
		}
		if history != "" && deleted {
			return usagef("--history lists deletions anyway; drop --deleted")
		}
		if grep == "" && (raw || limit != 0) {
			return usagef("--raw and --limit go with --grep")
		}
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		loc := cfg.Location()
		atTime, err := parseTimeArg("at", at, loc, true)
		if err != nil {
			return err
		}
		st, err := e.openRead(cfg)
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
		ctx := cmd.Context()
		all, err := memoryProjects(ctx, st, cfg)
		if err != nil {
			return err
		}
		ps, err := selectProjects(all, projectRef)
		if err != nil {
			return err
		}
		byID := make(map[int64]memProject, len(ps))
		for _, p := range ps {
			byID[p.ID] = p
		}
		switch {
		case history != "":
			return e.memoryHistory(ctx, o, st, ps, history, atTime, loc)
		case grep != "":
			if len(ps) == 0 {
				return writeList(o, []MemoryHitJSON{})
			}
			hits, err := st.SearchMemory(ctx, grep, store.MemorySearchOptions{ProjectIDs: projectIDs(ps), Raw: raw, Limit: limit})
			if err != nil {
				return errKind(ErrKindSQL, err)
			}
			out := make([]MemoryHitJSON, 0, len(hits))
			for _, h := range hits {
				p := byID[h.ProjectID]
				out = append(out, MemoryHitJSON{Project: p.Name, ProjectKey: p.Key, Harness: p.Harness, Path: h.Path, SHA256: NullString(h.SHA256),
					Changed: isoTime(h.Changed), Snippet: NullString(h.Snippet)})
			}
			if o.format.IsJSON() {
				return writeList(o, out)
			}
			for i, h := range out {
				_, _ = fmt.Fprintf(o.w, "%s  %s  %s\n    %s\n", localTime(hits[i].Changed, loc), o.cell(h.Project, 40), h.Path, o.cell(string(h.Snippet), o.width-4))
			}
			if len(out) == 0 {
				_, _ = fmt.Fprintln(e.app.Stderr, "no matches")
			}
			return nil
		}
		var files []store.MemoryFile
		if len(ps) > 0 {
			if files, err = st.MemoryFiles(ctx, store.MemoryFilter{ProjectIDs: projectIDs(ps), At: atTime, Deleted: deleted}); err != nil {
				return err
			}
		}
		out := make([]MemoryFileJSON, 0, len(files))
		for _, f := range files {
			p := byID[f.ProjectID]
			out = append(out, MemoryFileJSON{Project: p.Name, ProjectKey: p.Key, Harness: p.Harness, Cwd: NullString(p.Cwd), GitRoot: NullString(p.GitRoot),
				Path: f.Path, SHA256: NullString(f.SHA256), Bytes: f.Bytes, Deleted: f.Deleted, Changed: isoTime(f.Changed), Versions: f.Versions})
		}
		// By project name (the store orders by ID), then path.
		sort.SliceStable(out, func(i, j int) bool { return out[i].Project < out[j].Project })
		if o.format.IsJSON() {
			return writeList(o, out)
		}
		rows := make([][]string, 0, len(out))
		for _, f := range out {
			size := sizeLabel(f.Bytes)
			if f.Deleted {
				size = "deleted"
			}
			changed, _ := time.Parse(store.TimeFormat, f.Changed)
			rows = append(rows, []string{o.cell(f.Project, 40), f.Path, localTime(changed, loc), size, strconv.Itoa(f.Versions)})
		}
		if len(rows) == 0 {
			_, _ = fmt.Fprintln(e.app.Stderr, "no memory files")
			return nil
		}
		return o.table([]string{"PROJECT", "FILE", "CHANGED", "SIZE", "VERSIONS"}, rows)
	})
	of.register(c)
	f := c.Flags()
	f.StringVar(&projectRef, "project", "", "only projects whose name, path or harness key matches (exactly, else as a part)")
	f.StringVar(&at, "at", "", "the files as they were at this time (YYYY-MM-DD, YYYY-MM-DDTHH:MM, local time)")
	f.BoolVar(&deleted, "deleted", false, "include deleted files")
	f.StringVar(&history, "history", "", "the versions of this file (path relative to the memory directory)")
	f.StringVar(&grep, "grep", "", "full-text search over the current content (all words must occur)")
	f.BoolVar(&raw, "raw", false, "with --grep: pass WORDS to FTS5 unchanged (AND/OR/NOT, phrases, prefix*)")
	f.IntVar(&limit, "limit", 0, "with --grep: at most this many hits (0 = all)")
	return c
}

// memoryHistory writes the versions of file in the one selected project
// that has it; up to at when given.
func (e *env) memoryHistory(ctx context.Context, o *output, st *store.Store, ps []memProject, file string, at time.Time, loc *time.Location) error {
	var found []memProject
	var versions [][]store.MemoryVersion
	for _, p := range ps {
		vs, err := st.MemoryVersions(ctx, p.ID, file)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		found = append(found, p)
		versions = append(versions, vs)
	}
	switch len(found) {
	case 0:
		return errKind(ErrKindNotFound, fmt.Errorf("no memory file %q", file))
	case 1:
	default:
		names := make([]string, len(found))
		for i, p := range found {
			names[i] = p.Name
		}
		return errKind(ErrKindAmbiguous, fmt.Errorf("%q is in the memory of several projects (%s); choose one with --project", file, strings.Join(names, ", ")))
	}
	p := found[0]
	out := []MemoryVersionJSON{}
	var kept []store.MemoryVersion
	for _, v := range versions[0] {
		if !at.IsZero() && v.TS.After(at) {
			continue
		}
		kept = append(kept, v)
		out = append(out, MemoryVersionJSON{Project: p.Name, ProjectKey: p.Key, Harness: p.Harness, Path: file, TS: isoTime(v.TS), ObservedAt: isoTime(v.Observed),
			SHA256: NullString(v.SHA256), Bytes: v.Bytes, Deleted: v.Deleted})
	}
	if o.format.IsJSON() {
		return writeList(o, out)
	}
	rows := make([][]string, 0, len(out))
	for i, v := range out {
		size, sha := sizeLabel(v.Bytes), string(v.SHA256)
		if v.Deleted {
			size, sha = "deleted", ""
		}
		if len(sha) > 12 {
			sha = sha[:12]
		}
		vv := kept[i]
		rows = append(rows, []string{localTime(vv.TS, loc), localTime(vv.Observed, loc), size, sha, o.cell(p.Name, 40), file})
	}
	return o.table([]string{"CHANGED", "OBSERVED", "SIZE", "SHA256", "PROJECT", "FILE"}, rows)
}
