package project

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Patterns is a compiled set of glob patterns. A pattern is matched whole
// against a target: `*` matches any text, also across `/`, `?` one
// character, every other character is literal. The zero value matches
// nothing. Both [[project]] and [[worktime.category]] use this syntax.
type Patterns struct {
	res []*regexp.Regexp
}

// CompilePatterns compiles the patterns; at least one non-empty pattern is
// required.
func CompilePatterns(patterns []string) (Patterns, error) {
	if len(patterns) == 0 {
		return Patterns{}, fmt.Errorf("no match patterns")
	}
	var p Patterns
	for _, g := range patterns {
		if g == "" {
			return Patterns{}, fmt.Errorf("empty match pattern")
		}
		p.res = append(p.res, globRegexp(g))
	}
	return p, nil
}

// globRegexp translates a glob into an anchored regular expression.
func globRegexp(glob string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for _, c := range glob {
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// Matches reports whether any pattern matches any of the targets.
func (p Patterns) Matches(targets ...string) bool {
	for _, re := range p.res {
		for _, t := range targets {
			if re.MatchString(t) {
				return true
			}
		}
	}
	return false
}

// PaletteColor is one colour of the series palette: its name, as the
// configuration accepts it, and its value as #rrggbb.
type PaletteColor struct {
	Name, Hex string
}

// Palette holds the series colours in assignment order. The charts, the
// activity bars and the configuration (`color` of a [[project]]) share it,
// so a project looks alike everywhere.
var Palette = []PaletteColor{
	{"blue", "#4e79a7"},
	{"orange", "#f28e2b"},
	{"green", "#59a14f"},
	{"red", "#e15759"},
	{"purple", "#b07aa1"},
	{"teal", "#76b7b2"},
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ParseColor normalizes a configured colour to #rrggbb (lower case): a
// Palette name, case-insensitive, or a #rrggbb value. An empty string is
// no colour and parses to "".
func ParseColor(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	for _, c := range Palette {
		if strings.EqualFold(c.Name, s) {
			return c.Hex, nil
		}
	}
	if hexColor.MatchString(s) {
		return strings.ToLower(s), nil
	}
	names := make([]string, len(Palette))
	for i, c := range Palette {
		names[i] = c.Name
	}
	return "", fmt.Errorf("color %q: use %s or #rrggbb", s, strings.Join(names, ", "))
}

// Definition is one configured project ([[project]]): its name, an optional
// description and colour, and the patterns that assign sessions to it.
// Build it with NewDefinition.
type Definition struct {
	Name        string
	Description string
	// Color is the configured colour as #rrggbb, "" for the automatic one.
	Color string
	// Match holds the patterns as configured; see CompilePatterns for the
	// syntax and Resolver.Key for what they are matched against.
	Match    []string
	patterns Patterns
}

// NewDefinition validates and compiles a definition: the name and at least
// one non-empty pattern are required, the colour must parse (ParseColor).
func NewDefinition(name, description, color string, match []string) (Definition, error) {
	if strings.TrimSpace(name) == "" {
		return Definition{}, fmt.Errorf("project without a name")
	}
	hex, err := ParseColor(color)
	if err != nil {
		return Definition{}, fmt.Errorf("project %q: %w", name, err)
	}
	p, err := CompilePatterns(match)
	if err != nil {
		return Definition{}, fmt.Errorf("project %q: %w", name, err)
	}
	return Definition{Name: name, Description: description, Color: hex, Match: match, patterns: p}, nil
}

// Matches reports whether any pattern of the definition matches any target.
func (d Definition) Matches(targets ...string) bool { return d.patterns.Matches(targets...) }

// Resolver assigns the raw project keys of the archive (a session's git
// root, else its working directory) to configured projects. The zero value
// has no definitions and leaves every key as it is, so all display code
// can use one unconditionally. Resolvers are values; copy them freely.
type Resolver struct {
	defs   []Definition
	byName map[string]int
	home   string
}

// NewResolver builds a resolver from the definitions in file order; the
// first matching definition wins. home is the home directory that "~"
// stands for in the patterns. Two definitions with the same name are an
// error. Without definitions the result is the zero value.
func NewResolver(defs []Definition, home string) (Resolver, error) {
	if len(defs) == 0 {
		return Resolver{}, nil
	}
	r := Resolver{defs: defs, byName: make(map[string]int, len(defs)), home: home}
	for i, d := range defs {
		if _, dup := r.byName[d.Name]; dup {
			return Resolver{}, fmt.Errorf("project %q is listed twice", d.Name)
		}
		r.byName[d.Name] = i
	}
	return r, nil
}

// Empty reports whether the resolver has no definitions.
func (r Resolver) Empty() bool { return len(r.defs) == 0 }

// Definitions returns the definitions in file order.
func (r Resolver) Definitions() []Definition { return r.defs }

// Definition returns the definition of a configured project name.
func (r Resolver) Definition(name string) (Definition, bool) {
	i, ok := r.byName[name]
	if !ok {
		return Definition{}, false
	}
	return r.defs[i], true
}

// Color returns the configured colour (#rrggbb) of a project name, or ""
// when the name is not configured or has no colour.
func (r Resolver) Color(name string) string {
	d, ok := r.Definition(name)
	if !ok {
		return ""
	}
	return d.Color
}

// Key returns the key a raw project key is counted under: the name of the
// first definition whose pattern matches the key as a path, the key with
// the home directory as "~", the remote URL (if any) or the name the key
// would get without definitions (the base name of a git root, the "~" path
// of a working directory); without a match the key itself. Keys that are
// not absolute paths, such as "" or the store's "other", are never
// mapped. Several keys that map to the same name are thereby merged into
// one project.
func (r Resolver) Key(key string, dir bool, remote string) string {
	if len(r.defs) == 0 || !filepath.IsAbs(key) {
		return key
	}
	var dirs []string
	if dir {
		dirs = []string{key}
	}
	targets := []string{key, Tilde(key, r.home), Names([]string{key}, dirs, r.home)[key]}
	if remote != "" {
		targets = append(targets, remote)
	}
	for _, d := range r.defs {
		if d.Matches(targets...) {
			return d.Name
		}
	}
	return key
}

// Names is Names for a mix of configured names (as Key returns them) and
// raw keys: a configured name maps to itself, the raw keys get their
// derived names as without definitions. A derived name that equals a
// configured name among keys is shown as the key's path (with home as
// "~"), so two projects never share a display name.
func (r Resolver) Names(keys, dirs []string, home string) map[string]string {
	if len(r.defs) == 0 {
		return Names(keys, dirs, home)
	}
	out := make(map[string]string, len(keys))
	configured := map[string]bool{}
	var raw []string
	for _, k := range keys {
		if _, ok := r.byName[k]; ok {
			out[k] = k
			configured[k] = true
			continue
		}
		raw = append(raw, k)
	}
	for k, n := range Names(raw, dirs, home) {
		if configured[n] && filepath.IsAbs(k) {
			n = Tilde(k, home)
		}
		out[k] = n
	}
	return out
}
