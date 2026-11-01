package search

import (
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/store"
)

// actors are the actor filter cycle; "" means all.
var actors = []string{"", "human", "agent", "peer"}

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
	actor int    // index into actors
	span  int    // index into spans
	repo  string // substring of remote URL, git root or cwd
	raw   bool   // FTS5 syntax passed through
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
	if f.raw {
		s += "  raw"
	}
	return s
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
