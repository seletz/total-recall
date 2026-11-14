package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/seletz/total-recall/internal/account"
	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/store"
)

// accountResolver builds the resolver of login names and groups (config
// tables and the registry); registry entries that had to be skipped are
// reported on stderr. A broken registry is a config error.
func (e *env) accountResolver(cfg *config.Config) (*account.Resolver, error) {
	r, warns, err := cfg.AccountResolver()
	if err != nil {
		return nil, errKind(ErrKindConfig, err)
	}
	for _, w := range warns {
		_, _ = fmt.Fprintln(e.app.Stderr, "warning:", w)
	}
	return r, nil
}

// applyOwners turns --account and --account-group into store filters. A
// group selects the logins (of the archive) the resolver puts in it; an
// account reference that is the name of a login (the name of an account,
// an organization or a pair, or "account/organization") selects those
// logins, any other reference stays the account ID prefix of the filter. A
// group that is not configured is a usage error; one without sessions
// selects nothing.
func applyOwners(ctx context.Context, st *store.Store, r *account.Resolver, accountRef, group string, sf *store.SessionFilter) error {
	sf.Account = accountRef
	if accountRef == "" && group == "" {
		return nil
	}
	if group != "" {
		known := r.Groups()
		found := false
		for _, g := range known {
			found = found || g == group
		}
		if !found {
			return usagef("unknown account group %q (configured: %s)", group, strings.Join(known, ", "))
		}
	}
	owners, err := st.Owners(ctx)
	if err != nil {
		return err
	}
	var set []store.Owner
	pick := func(match func(o store.Owner) bool) []store.Owner {
		var out []store.Owner
		for _, o := range owners {
			if match(o) {
				out = append(out, o)
			}
		}
		return out
	}
	restrict := func(m []store.Owner) {
		if sf.Owners == nil {
			sf.Owners = &store.OwnerSet{Owners: m}
			return
		}
		sf.Owners = &store.OwnerSet{Owners: intersect(sf.Owners.Owners, m)}
	}
	if group != "" {
		set = pick(func(o store.Owner) bool { return r.Resolve(o.Account, o.Organization).Group == group })
		restrict(set)
	}
	if accountRef != "" {
		named := pick(func(o store.Owner) bool {
			return o != (store.Owner{}) && (account.Key(o.Account, o.Organization) == accountRef || r.Resolve(o.Account, o.Organization).Name == accountRef)
		})
		if len(named) > 0 {
			sf.Account = ""
			restrict(named)
		}
	}
	return nil
}

func intersect(a, b []store.Owner) []store.Owner {
	var out []store.Owner
	for _, x := range a {
		for _, y := range b {
			if x == y {
				out = append(out, x)
				break
			}
		}
	}
	return out
}

// ownerGroupFunc adapts a resolver to ActivityFilter.Group.
func ownerGroupFunc(r *account.Resolver) func(account, organization string) string {
	return func(a, o string) string { return r.Resolve(a, o).Group }
}

// ownerLabel formats an ID for text output: the name with the ID in
// parentheses, else the ID.
func ownerLabel(id, name string) string {
	if name != "" {
		return name + " (" + id + ")"
	}
	return id
}
