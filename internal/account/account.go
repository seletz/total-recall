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
// the path). Files are only read. expand resolves "~" in dir; nil keeps it.
func LoadRegistry(path string, expand func(string) string) (entries []Entry, warnings []string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("account registry: %w", err)
	}
	var doc struct {
		Accounts map[string]struct {
			Group string `json:"group"`
			Dir   string `json:"dir"`
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
		acc, org, err := readLogin(filepath.Join(dir, ".claude.json"))
		if err != nil || (acc == "" && org == "") {
			warnings = append(warnings, fmt.Sprintf("account registry: no login IDs for %s, skipped", name))
			continue
		}
		entries = append(entries, Entry{ID: acc, Organization: org, Name: name, Group: a.Group})
	}
	return entries, warnings, nil
}

// readLogin reads the IDs of the logged-in account from a .claude.json.
func readLogin(path string) (account, org string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	var doc struct {
		OAuth struct {
			Account string `json:"accountUuid"`
			Org     string `json:"organizationUuid"`
		} `json:"oauthAccount"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", "", err
	}
	return doc.OAuth.Account, doc.OAuth.Org, nil
}
