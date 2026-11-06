package cli

import (
	"context"
	"errors"
	"fmt"
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

// loadProjectCatalog builds the project catalog of the archive; a broken
// project definition is a config error.
func loadProjectCatalog(ctx context.Context, st *store.Store, cfg *config.Config) (*project.Catalog, error) {
	res, err := cfg.ProjectResolver()
	if err != nil {
		return nil, errKind(ErrKindConfig, err)
	}
	return project.LoadCatalog(ctx, st, res, cfg.Home)
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
func (e *env) writeTypedHits(o *output, groups []store.TypeHits, cat *project.Catalog, loc *time.Location) error {
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
					cat.Name(h.Project), h.Session.ID)
			case store.TypeMemory:
				head = fmt.Sprintf("%s  %s  %s", localTime(h.TS, loc), cat.MemoryName(h.Memory), h.Memory.Path)
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

func typedHitJSON(h store.Hit, cat *project.Catalog) any {
	switch h.Type {
	case store.TypeSession:
		s := h.Session
		return SessionSearchHitJSON{SessionID: s.ID, SessionName: NullString(s.DisplayName()), Repo: NullString(s.Repo()),
			TS: NullString(isoTime(s.StartedAt)), Snippet: NullString(h.Snippet), Rank: h.Rank, Type: string(h.Type),
			Project: NullString(cat.Name(h.Project)), Harness: s.Harness, EndedAt: NullString(isoTime(s.EndedAt)), Events: s.Events}
	case store.TypeMemory:
		m := h.Memory
		repo := h.Project.Remote
		if repo == "" {
			repo = h.Project.Key
		}
		return MemorySearchHitJSON{Repo: NullString(repo), TS: isoTime(m.Changed), Snippet: NullString(h.Snippet), Rank: h.Rank,
			Type: string(h.Type), Project: NullString(cat.MemoryName(m)), ProjectKey: m.ProjectKey, Path: m.Path, SHA256: NullString(m.SHA256)}
	}
	ev := h.Event
	return HitJSON{SessionID: ev.Event.SessionID, SessionName: NullString(ev.SessionName), Repo: NullString(ev.Repo),
		EventID: ev.Event.ID, TS: isoTime(ev.Event.TS), Actor: string(ev.Event.Actor), Kind: string(ev.Event.Kind),
		Snippet: NullString(ev.Snippet), Rank: ev.Rank, Type: string(h.Type), Project: NullString(cat.Name(h.Project))}
}
