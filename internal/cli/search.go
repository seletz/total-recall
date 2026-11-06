package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
)

// Typed search (#196): search --type T[,T…] and --project. The store
// searches every type on its own index; this file names the projects of
// the hits, resolves --project and writes the hits per type.

// SessionSearchHitJSON is a hit of type session in search --json. Every
// key is always present; a NullString without a value is null.
type SessionSearchHitJSON struct {
	SessionID   string     `json:"session_id"`
	SessionName NullString `json:"session_name"`
	Repo        NullString `json:"repo"`
	TS          NullString `json:"ts"` // start of the session
	Snippet     NullString `json:"snippet"`
	Rank        float64    `json:"rank"` // always 0
	Type        string     `json:"type"`
	Project     NullString `json:"project"`
	Harness     string     `json:"harness"`
	EndedAt     NullString `json:"ended_at"`
	Events      int        `json:"events"`
}

// MemorySearchHitJSON is a hit of type memory in search --json: a memory
// file whose current content matches. Every key is always present; the
// session keys are always null.
type MemorySearchHitJSON struct {
	SessionID   NullString `json:"session_id"`
	SessionName NullString `json:"session_name"`
	Repo        NullString `json:"repo"`
	TS          string     `json:"ts"` // time of the current version
	Snippet     NullString `json:"snippet"`
	Rank        float64    `json:"rank"`
	Type        string     `json:"type"`
	Project     NullString `json:"project"`
	ProjectKey  string     `json:"project_key"`
	Path        string     `json:"path"`
	SHA256      NullString `json:"sha256"`
}

// projectCatalog names raw project keys like stats --by project (the
// configured definition, else the derived name) over every key of the
// archive and of the memory, and resolves --project.
type projectCatalog struct {
	res   project.Resolver
	names map[string]string // mapped key -> display name
	keys  []catalogKey
	mem   []catalogMemory
}

type catalogKey struct {
	ref  store.ProjectRef
	name string
}

type catalogMemory struct {
	p    store.MemoryProject
	ref  store.ProjectRef
	name string
}

func memoryRef(p store.MemoryProject) store.ProjectRef {
	switch {
	case p.GitRoot != "":
		return store.ProjectRef{Key: p.GitRoot, Remote: p.GitRemoteURL}
	case p.Cwd != "":
		return store.ProjectRef{Key: p.Cwd, Dir: true}
	}
	return store.ProjectRef{}
}

func loadProjectCatalog(ctx context.Context, st *store.Store, cfg *config.Config) (*projectCatalog, error) {
	res, err := cfg.ProjectResolver()
	if err != nil {
		return nil, errKind(ErrKindConfig, err)
	}
	pks, err := st.ProjectKeys(ctx)
	if err != nil {
		return nil, err
	}
	mps, err := st.MemoryProjects(ctx)
	if err != nil {
		return nil, err
	}
	c := &projectCatalog{res: res}
	var refs []store.ProjectRef
	for _, k := range pks {
		r := store.ProjectRef{Key: k.Key, Dir: k.Dir, Remote: k.Remote}
		if r.Dir {
			r.Remote = ""
		}
		refs = append(refs, r)
		c.keys = append(c.keys, catalogKey{ref: r})
	}
	for _, p := range mps {
		r := memoryRef(p)
		c.mem = append(c.mem, catalogMemory{p: p, ref: r})
		if r.Key != "" {
			refs = append(refs, r)
		}
	}
	var mapped, dirs []string
	for _, r := range refs {
		m := res.Key(r.Key, r.Dir, r.Remote)
		mapped = append(mapped, m)
		if r.Dir && m == r.Key {
			dirs = append(dirs, r.Key)
		}
	}
	c.names = res.Names(mapped, dirs, cfg.Home)
	for i := range c.keys {
		c.keys[i].name = c.name(c.keys[i].ref)
	}
	for i := range c.mem {
		c.mem[i].name = c.mem[i].p.Key
		if c.mem[i].ref.Key != "" {
			c.mem[i].name = c.name(c.mem[i].ref)
		}
	}
	return c, nil
}

// name is the display name of a raw project key, "" for none.
func (c *projectCatalog) name(r store.ProjectRef) string {
	if r.Key == "" {
		return ""
	}
	m := c.res.Key(r.Key, r.Dir, r.Remote)
	if n, ok := c.names[m]; ok {
		return n
	}
	return m
}

// memoryName is the display name of a memory hit's project: its project
// name, else the harness's native key.
func (c *projectCatalog) memoryName(h *store.MemoryHit) string {
	if n := c.name(h.Project); n != "" {
		return n
	}
	return h.ProjectKey
}

// scope resolves --project: the raw keys and memory projects whose name,
// key, path, remote URL or native memory key equals ref, else those that
// contain it. A memory project and the sessions with
// the same raw key go together.
func (c *projectCatalog) scope(ref string) (*store.ProjectScope, error) {
	pick := func(match func(string) bool) *store.ProjectScope {
		sc := &store.ProjectScope{Keys: []string{}, MemoryProjectIDs: []int64{}}
		keys := map[string]bool{}
		for _, k := range c.keys {
			if match(k.name) || match(k.ref.Key) || match(k.ref.Remote) {
				keys[k.ref.Key] = true
			}
		}
		for _, m := range c.mem {
			if match(m.name) || match(m.p.Key) || match(m.p.Cwd) || match(m.p.GitRoot) || match(m.p.GitRemoteURL) {
				sc.MemoryProjectIDs = append(sc.MemoryProjectIDs, m.p.ID)
				if m.ref.Key != "" {
					keys[m.ref.Key] = true
				}
			}
		}
		for _, k := range c.keys {
			if keys[k.ref.Key] {
				sc.Keys = append(sc.Keys, k.ref.Key)
			}
		}
		for _, m := range c.mem {
			if m.ref.Key != "" && keys[m.ref.Key] && !containsID(sc.MemoryProjectIDs, m.p.ID) {
				sc.MemoryProjectIDs = append(sc.MemoryProjectIDs, m.p.ID)
			}
		}
		if len(keys) == 0 && len(sc.MemoryProjectIDs) == 0 {
			return nil
		}
		return sc
	}
	if sc := pick(func(s string) bool { return s != "" && s == ref }); sc != nil {
		return sc, nil
	}
	if sc := pick(func(s string) bool { return s != "" && strings.Contains(s, ref) }); sc != nil {
		return sc, nil
	}
	return nil, errKind(ErrKindNotFound, fmt.Errorf("no project matches %q", ref))
}

func containsID(ids []int64, id int64) bool {
	for _, i := range ids {
		if i == id {
			return true
		}
	}
	return false
}

// searchFlagValues tells which filter flags of search are set, by store
// filter name (= flag name).
func searchFlagValues(f filterFlags, project, session, actor string, raw bool) map[string]bool {
	return map[string]bool{
		store.FilterSince: f.since != "", store.FilterUntil: f.until != "", store.FilterRepo: f.repo != "",
		store.FilterHarness: f.harness != "", store.FilterName: f.name != "", store.FilterAccount: f.account != "",
		store.FilterProject: project != "", store.FilterSession: session != "", store.FilterActor: actor != "", store.FilterRaw: raw,
	}
}

// checkSearchFlags rejects a set flag that a selected type does not apply.
func checkSearchFlags(types []store.HitType, set map[string]bool) error {
	order := []string{store.FilterSince, store.FilterUntil, store.FilterRepo, store.FilterHarness, store.FilterName, store.FilterAccount,
		store.FilterProject, store.FilterSession, store.FilterActor, store.FilterRaw}
	for _, t := range types {
		for _, f := range order {
			if set[f] && !store.Supports(t, f) {
				return usagef("--%s does not apply to type %s", f, t)
			}
		}
	}
	return nil
}

// searchError maps an error of SearchTyped to an error kind.
func searchError(err error) error {
	var fe *store.FilterError
	if errors.As(err, &fe) {
		return usagef("--%s does not apply to type %s", fe.Filter, fe.Type)
	}
	return errKind(ErrKindSQL, err)
}

// writeTypedHits writes the groups of a typed search: JSON with one flat
// object per hit, or text. Text with only events keeps the event lines
// of earlier versions; with other types every line starts with the
// type. A group cut off by --limit gets a note on stderr.
func (e *env) writeTypedHits(o *output, groups []store.TypeHits, cat *projectCatalog, loc *time.Location) error {
	if o.format.IsJSON() {
		var out []any
		for _, g := range groups {
			for _, h := range g.Hits {
				out = append(out, typedHitJSON(h, cat))
			}
		}
		if out == nil {
			out = []any{}
		}
		return writeList(o, out)
	}
	onlyEvents := len(groups) == 1 && groups[0].Type == store.TypeEvent
	w, n := o.w, 0
	for _, g := range groups {
		for _, h := range g.Hits {
			n++
			prefix := ""
			if !onlyEvents {
				prefix = string(h.Type) + "  "
			}
			var head string
			switch h.Type {
			case store.TypeEvent:
				head = fmt.Sprintf("%s  %s  %s  %s  [%s]", localTime(h.TS, loc), o.cell(h.Event.SessionName, 40),
					shortRepo(h.Event.Repo), h.Event.Event.SessionID, h.Event.Event.Actor)
			case store.TypeSession:
				head = fmt.Sprintf("%s  %s  %s  %s", localTime(h.TS, loc), o.cell(h.Session.DisplayName(), 40),
					cat.name(h.Project), h.Session.ID)
			case store.TypeMemory:
				head = fmt.Sprintf("%s  %s  %s", localTime(h.TS, loc), cat.memoryName(h.Memory), h.Memory.Path)
			}
			// The snippet line is indented by four spaces.
			_, _ = fmt.Fprintf(w, "%s%s\n    %s\n", prefix, head, o.cell(h.Snippet, o.width-4))
		}
	}
	for _, g := range groups {
		if g.Total > len(g.Hits) {
			_, _ = fmt.Fprintf(e.app.Stderr, "%s: %d of %d hits, --limit 0 for all\n", g.Type, len(g.Hits), g.Total)
		}
	}
	if n == 0 {
		_, _ = fmt.Fprintln(e.app.Stderr, "no matches")
	}
	return nil
}

func typedHitJSON(h store.Hit, cat *projectCatalog) any {
	switch h.Type {
	case store.TypeSession:
		s := h.Session
		return SessionSearchHitJSON{SessionID: s.ID, SessionName: NullString(s.DisplayName()), Repo: NullString(s.Repo()),
			TS: NullString(isoTime(s.StartedAt)), Snippet: NullString(h.Snippet), Rank: h.Rank, Type: string(h.Type),
			Project: NullString(cat.name(h.Project)), Harness: s.Harness, EndedAt: NullString(isoTime(s.EndedAt)), Events: s.Events}
	case store.TypeMemory:
		m := h.Memory
		repo := h.Project.Remote
		if repo == "" {
			repo = h.Project.Key
		}
		return MemorySearchHitJSON{Repo: NullString(repo), TS: isoTime(m.Changed), Snippet: NullString(h.Snippet), Rank: h.Rank,
			Type: string(h.Type), Project: NullString(cat.memoryName(m)), ProjectKey: m.ProjectKey, Path: m.Path, SHA256: NullString(m.SHA256)}
	}
	ev := h.Event
	return HitJSON{SessionID: ev.Event.SessionID, SessionName: NullString(ev.SessionName), Repo: NullString(ev.Repo),
		EventID: ev.Event.ID, TS: isoTime(ev.Event.TS), Actor: string(ev.Event.Actor), Kind: string(ev.Event.Kind),
		Snippet: NullString(ev.Snippet), Rank: ev.Rank, Type: string(h.Type), Project: NullString(cat.name(h.Project))}
}
