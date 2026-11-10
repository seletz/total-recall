package search

import (
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/store"
)

// actors are the actor filter cycle; "" means all.
var actors = []string{"", "human", "agent", "peer", "tool"}

// attachKinds are the attachment filter cycle: off, any attachment, only
// images, only documents.
var attachKinds = []string{"", "any", "image", "document"}

// span is one entry of the time filter cycle; a zero Duration means all.
type span struct {
	label string
	d     time.Duration
}

var spans = []span{
	{"all", 0},
	{"7d", 7 * 24 * time.Hour},
	{"30d", 30 * 24 * time.Hour},
	{"90d", 90 * 24 * time.Hour},
}

// filters are the toggles of the hit list. The zero value filters nothing.
type filters struct {
	actor  int    // index into actors
	attach int    // index into attachKinds; 0 is off
	span   int    // index into spans
	repo   string // substring of remote URL, git root or cwd
	raw    bool   // FTS5 syntax passed through
}

// label is the text shown at the right end of the input line.
func (f filters) label() string {
	actor, repo := actors[f.actor], f.repo
	if actor == "" {
		actor = "all"
	}
	if repo == "" {
		repo = "*"
	}
	s := "actor: " + actor + "  since: " + spans[f.span].label + "  repo: " + repo
	if k := attachKinds[f.attach]; k != "" {
		s += "  attach: " + k
	}
	if f.raw {
		s += "  raw"
	}
	return s
}

// attachment returns the attachment filter for the store, nil when it is
// off. The actor is the actor of the event with the attachment: the actor
// filter doubles as that (human for what the user pasted, tool for the
// screenshots of tool results).
func (f filters) attachment() *store.AttachmentFilter {
	if f.attach == 0 {
		return nil
	}
	kind, _ := store.ParseAttachmentKind(attachKinds[f.attach])
	return &store.AttachmentFilter{Kind: kind, Actor: actors[f.actor]}
}

// apply writes the filters into a search filter; now anchors the time span.
func (f filters) apply(sf *store.SearchFilter, now time.Time) {
	sf.Actor = actors[f.actor]
	sf.Repo = f.repo
	sf.Raw = f.raw
	if d := spans[f.span].d; d > 0 {
		sf.Since = now.Add(-d)
	}
}

// terms returns the words of query that the preview and the detail view
// highlight. In raw mode FTS5 operators, quotes, parentheses and prefix
// stars are dropped; otherwise every whitespace-separated word is a term,
// as the store searches for each of them literally.
func terms(query string, raw bool) []string {
	var out []string
	for _, f := range strings.Fields(query) {
		if raw {
			f = strings.Trim(f, `"*(),`)
			switch strings.ToUpper(f) {
			case "", "AND", "OR", "NOT", "NEAR":
				continue
			}
			if strings.HasPrefix(strings.ToUpper(f), "NEAR(") {
				f = f[len("NEAR("):]
			}
		}
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
