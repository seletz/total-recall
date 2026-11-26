package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/tui/memview"
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

// MemoryEventJSON is one change of `memory --timeline --json`, newest
// first over all selected projects: the same events as the timeline of the
// TUI. Kind is added (first version, or back after a deletion), changed or
// deleted. TS and ObservedAt are as in MemoryVersionJSON; SHA256 and Bytes
// describe the version (null and 0 for a deletion), PrevTS and PrevSHA256
// the version it replaces (null unless changed). DeltaBytes is the change
// in size, so the deltas of a file add up to its size now.
type MemoryEventJSON struct {
	Project    string     `json:"project"`
	ProjectKey string     `json:"project_key"`
	Harness    string     `json:"harness"`
	Path       string     `json:"path"`
	Kind       string     `json:"kind"`
	TS         string     `json:"ts"`
	ObservedAt string     `json:"observed_at"`
	SHA256     NullString `json:"sha256"`
	Bytes      int64      `json:"bytes"`
	PrevTS     NullString `json:"prev_ts"`
	PrevSHA256 NullString `json:"prev_sha256"`
	DeltaBytes int64      `json:"delta_bytes"`
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
	var deleted, raw, timeline bool
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
  memory --timeline            every change of every file (added, changed,
                               deleted), newest first; with --at only up to DATE

--project narrows to the projects whose name, path or harness key matches
(exactly, else as a part). The content of a version: blob SHA256.`,
		Args: usageArgs(cobra.NoArgs),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		if timeline && (history != "" || grep != "" || deleted) {
			return usagef("--timeline lists every change including deletions; it does not combine with --history, --grep or --deleted")
		}
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
		case timeline:
			return e.memoryTimeline(ctx, o, st, ps, atTime, loc)
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
	f.BoolVar(&timeline, "timeline", false, "every change of every file, newest first (as the timeline of the TUI)")
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

// memoryTimeline writes the changes of all files of the selected projects,
// newest first, built by the same memview.Timeline as the TUI; up to at
// when given. Every version is one event, so the events of a file are its
// versions.
func (e *env) memoryTimeline(ctx context.Context, o *output, st *store.Store, ps []memProject, at time.Time, loc *time.Location) error {
	type item struct {
		p  memProject
		ev memview.Event
	}
	var items []item
	for _, p := range ps {
		files, err := st.MemoryFiles(ctx, store.MemoryFilter{ProjectIDs: []int64{p.ID}, Deleted: true})
		if err != nil {
			return err
		}
		versions := make(map[string][]store.MemoryVersion, len(files))
		for _, f := range files {
			vs, err := st.MemoryVersions(ctx, p.ID, f.Path)
			if err != nil {
				return err
			}
			if !at.IsZero() {
				vs = slices.DeleteFunc(vs, func(v store.MemoryVersion) bool { return v.TS.After(at) })
			}
			if len(vs) > 0 {
				versions[f.Path] = vs
			}
		}
		for _, ev := range memview.Timeline(versions) {
			items = append(items, item{p, ev})
		}
	}
	slices.SortStableFunc(items, func(a, b item) int {
		if c := b.ev.Version.TS.Compare(a.ev.Version.TS); c != 0 {
			return c
		}
		if a.ev.Version.Seq != b.ev.Version.Seq {
			return cmp.Compare(b.ev.Version.Seq, a.ev.Version.Seq)
		}
		return strings.Compare(a.p.Name+"\x00"+a.ev.Path, b.p.Name+"\x00"+b.ev.Path)
	})
	out := make([]MemoryEventJSON, 0, len(items))
	for _, it := range items {
		v := it.ev.Version
		j := MemoryEventJSON{Project: it.p.Name, ProjectKey: it.p.Key, Harness: it.p.Harness, Path: it.ev.Path, Kind: it.ev.Kind.String(),
			TS: isoTime(v.TS), ObservedAt: isoTime(v.Observed), SHA256: NullString(v.SHA256), Bytes: v.Bytes, DeltaBytes: it.ev.Delta}
		if it.ev.Kind == memview.Changed {
			j.PrevTS, j.PrevSHA256 = NullString(isoTime(it.ev.Prev)), NullString(it.ev.PrevSHA)
		}
		out = append(out, j)
	}
	if o.format.IsJSON() {
		return writeList(o, out)
	}
	if len(out) == 0 {
		_, _ = fmt.Fprintln(e.app.Stderr, "no memory changes")
		return nil
	}
	rows := make([][]string, len(out))
	for i, j := range out {
		d := fmt.Sprintf("%+d", j.DeltaBytes)
		rows[i] = []string{localTime(items[i].ev.Version.TS, loc), j.Kind, o.cell(j.Project, 40), j.Path, d}
	}
	return o.table([]string{"CHANGED", "KIND", "PROJECT", "FILE", "BYTES"}, rows)
}
