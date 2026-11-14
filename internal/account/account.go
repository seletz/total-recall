// Package account names and groups the logins of a harness. A login is an
// account ID plus an organization ID; one account can belong to several
// organizations (a single and a team subscription of the same person share
// the account ID), so a name or a group attaches to an account, an
// organization or the pair. The package is pure: where the entries come
// from (config.toml, a login registry) is decided by the caller.
package account

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry names and groups an account, an organization or a pair. ID and
// Organization are matched exactly; an empty one matches any value, so an
// entry with only an ID applies to every organization of the account. AnyID
// is the legacy form of the [accounts] table, whose keys are account or
// organization IDs without telling which.
type Entry struct {
	ID, Organization string
	AnyID            string
	Name, Group      string
}

// Identity is what a Resolver knows about a login.
type Identity struct {
	// Name names the account or the pair; "" without an entry.
	Name string
	// Group is the group the login belongs to; "" without one.
	Group string
	// OrganizationName names the organization alone (an entry with only
	// an organization); "" without one.
	OrganizationName string
}

// Resolver looks up names and groups. Entries of the first list (the
// config) win over the second (the registry); within a list the entry that
// fits best wins: the pair before the account before the organization.
type Resolver struct {
	lists [2][]Entry
}

// New builds a Resolver from the config entries and the registry entries.
func New(config, registry []Entry) *Resolver {
	return &Resolver{lists: [2][]Entry{config, registry}}
}

// rank says how well e fits the login: 0 pair, 1 account, 2 organization,
// -1 not at all.
func (e Entry) rank(account, org string) int {
	switch {
	case e.AnyID != "":
		switch {
		case account != "" && e.AnyID == account:
			return 1
		case org != "" && e.AnyID == org:
			return 2
		}
		return -1
	case e.ID == "" && e.Organization == "":
		return -1
	case e.ID != "" && e.ID != account, e.Organization != "" && e.Organization != org:
		return -1
	case e.ID != "" && e.Organization != "":
		return 0
	case e.ID != "":
		return 1
	}
	return 2
}

// Resolve returns the name and group of a login. A nil Resolver knows
// nothing.
func (r *Resolver) Resolve(account, org string) Identity {
	var id Identity
	if r == nil {
		return id
	}
	for _, list := range r.lists {
		for _, rank := range []int{0, 1, 2} {
			for _, e := range list {
				if e.rank(account, org) != rank {
					continue
				}
				if id.Name == "" {
					id.Name = e.Name
				}
				if id.Group == "" {
					id.Group = e.Group
				}
			}
		}
	}
	if org != "" {
		for _, list := range r.lists {
			for _, e := range list {
				if id.OrganizationName == "" && e.Name != "" && ((e.ID == "" && e.Organization == org && e.AnyID == "") || e.AnyID == org) {
					id.OrganizationName = e.Name
				}
			}
		}
	}
	return id
}

// Groups returns the configured group names, sorted.
func (r *Resolver) Groups() []string {
	set := map[string]bool{}
	if r != nil {
		for _, list := range r.lists {
			for _, e := range list {
				if e.Group != "" {
					set[e.Group] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for g := range set {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// Key is the one-string form of a login used as a dimension key and in
// config keys: "account/organization", the account alone without an
// organization.
func Key(account, org string) string {
	if org == "" {
		return account
	}
	return account + "/" + org
}

// SplitKey is the inverse of Key.
func SplitKey(key string) (account, org string) {
	account, org, _ = strings.Cut(key, "/")
	return account, org
}

// FromNames converts the legacy [accounts] table (ID → name, or
// "account/organization" → name) to entries, sorted by key.
func FromNames(names map[string]string) []Entry {
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Entry, 0, len(keys))
	for _, k := range keys {
		if a, o, ok := strings.Cut(k, "/"); ok {
			out = append(out, Entry{ID: a, Organization: o, Name: names[k]})
			continue
		}
		out = append(out, Entry{AnyID: k, Name: names[k]})
	}
	return out
}

// LoadRegistry reads a JSON login registry,
//
//	{"accounts": {"<name>": {"group": "…", "label": "…", "short": "…", "dir": "…"}}}
//
// and returns one entry per login whose IDs it could read from
// <dir>/.claude.json (oauthAccount.accountUuid and organizationUuid). The
// key of the registry is the name, sorted. Logins without a readable
// directory or IDs are skipped with a warning (which names the entry, not
// the path). Two entries with the same login (account and organization): an
// entry with "pick": false is an alias (not selectable in the login picker)
// and loses against one without it, silently; among equals the first by
// name wins and a warning says so. The same account in different
// organizations is the normal case and no collision. The organization name
// of the .claude.json (oauthAccount.organizationName) names the
// organization (an entry of its own, so every name of a config goes first).
//
// The harness keeps the login in <dir>/.claude.json, except for its default
// directory <home>/.claude (no CLAUDE_CONFIG_DIR): there it is
// <home>/.claude.json, and a file inside the directory may be stale. home
// is that home directory; "" has no default directory. Files are only read.
// expand resolves "~" in dir; nil keeps it.
func LoadRegistry(path, home string, expand func(string) string) (entries []Entry, warnings []string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("account registry: %w", err)
	}
	var doc struct {
		Accounts map[string]struct {
			Group string `json:"group"`
			Dir   string `json:"dir"`
			Pick  *bool  `json:"pick"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("account registry %s: %w", filepath.Base(path), err)
	}
	names := make([]string, 0, len(doc.Accounts))
	for n := range doc.Accounts {
		names = append(names, n)
	}
	sort.Strings(names)
	type cand struct {
		name, group, orgName string
		acc, org             string
		alias                bool
	}
	byLogin := map[string][]cand{} // account/organization -> entries, by name
	var order []string             // logins in the order of their first entry
	for _, name := range names {
		a := doc.Accounts[name]
		if a.Dir == "" {
			warnings = append(warnings, fmt.Sprintf("account registry: %s has no dir, skipped", name))
			continue
		}
		dir := a.Dir
		if expand != nil {
			dir = expand(dir)
		}
		file := filepath.Join(dir, ".claude.json")
		if home != "" && filepath.Clean(dir) == filepath.Join(home, ".claude") {
			file = filepath.Join(home, ".claude.json")
		}
		acc, org, orgName, err := readLogin(file)
		if err != nil || (acc == "" && org == "") {
			warnings = append(warnings, fmt.Sprintf("account registry: no login IDs for %s, skipped", name))
			continue
		}
		k := Key(acc, org)
		if _, ok := byLogin[k]; !ok {
			order = append(order, k)
		}
		byLogin[k] = append(byLogin[k], cand{name, a.Group, orgName, acc, org, a.Pick != nil && !*a.Pick})
	}
	// One entry per login: an alias ("pick": false) loses against an entry
	// without it; among equals the first by name wins, with a warning.
	orgNamed := map[string]bool{}
	var orgEntries []Entry
	won := map[string]cand{}
	for _, k := range order {
		cs := byLogin[k]
		var pickable []cand
		for _, c := range cs {
			if !c.alias {
				pickable = append(pickable, c)
			}
		}
		pool := pickable
		if len(pool) == 0 {
			pool = cs
		}
		w := pool[0]
		if len(pool) > 1 {
			names := make([]string, len(pool)-1)
			for i, c := range pool[1:] {
				names[i] = c.name
			}
			warnings = append(warnings, fmt.Sprintf("account registry: %s and %s have the same login; %s wins (the first by name)", w.name, strings.Join(names, ", "), w.name))
		}
		won[k] = w
	}
	// the entries in the order of the names
	sort.Slice(order, func(i, j int) bool { return won[order[i]].name < won[order[j]].name })
	for _, k := range order {
		w := won[k]
		entries = append(entries, Entry{ID: w.acc, Organization: w.org, Name: w.name, Group: w.group})
		for _, c := range byLogin[k] {
			if c.orgName != "" && c.org != "" && !orgNamed[c.org] {
				orgNamed[c.org] = true
				orgEntries = append(orgEntries, Entry{Organization: c.org, Name: c.orgName})
			}
		}
	}
	return append(entries, orgEntries...), warnings, nil
}

// readLogin reads the IDs of the logged-in account from a .claude.json.
func readLogin(path string) (account, org, orgName string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", err
	}
	var doc struct {
		OAuth struct {
			Account string `json:"accountUuid"`
			Org     string `json:"organizationUuid"`
			OrgName string `json:"organizationName"`
		} `json:"oauthAccount"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", "", "", err
	}
	return doc.OAuth.Account, doc.OAuth.Org, doc.OAuth.OrgName, nil
}
