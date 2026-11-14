package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Owner is the login a session ran on: account ID and organization ID, as
// the session-owner record reported them. Both are empty for sessions
// without one.
type Owner struct {
	Account, Organization string
}

// OwnerSet restricts a query to the sessions of these owners (the effective
// owner, see SessionInfo.Owner); an empty set matches no session.
type OwnerSet struct {
	Owners []Owner
}

// ownerExprs are the SQL expressions of the effective account and
// organization ID of the session aliased s: its own, or — when it carries
// neither — those of its parent session. pAlias names a joined parent
// session; "" reads the parent with a subquery.
func ownerExprs(pAlias string) (account, org string) {
	pa, po := pAlias+".account_id", pAlias+".organization_id"
	if pAlias == "" {
		pa = "(SELECT x.account_id FROM sessions x WHERE x.id = s.parent_id)"
		po = "(SELECT x.organization_id FROM sessions x WHERE x.id = s.parent_id)"
	}
	const own = "COALESCE(s.account_id, '') = '' AND COALESCE(s.organization_id, '') = ''"
	return "CASE WHEN " + own + " THEN COALESCE(" + pa + ", '') ELSE COALESCE(s.account_id, '') END",
		"CASE WHEN " + own + " THEN COALESCE(" + po + ", '') ELSE COALESCE(s.organization_id, '') END"
}

// cond returns the SQL condition and arguments for the session aliased s;
// pAlias is as for ownerExprs.
func (o *OwnerSet) cond(pAlias string) (string, []any) {
	if len(o.Owners) == 0 {
		return "0", nil
	}
	acc, org := ownerExprs(pAlias)
	parts := make([]string, len(o.Owners))
	args := make([]any, 0, 2*len(o.Owners))
	for i, w := range o.Owners {
		parts[i] = "(" + acc + " = ? AND " + org + " = ?)"
		args = append(args, w.Account, w.Organization)
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// Owner returns the effective owner of the session: its own account and
// organization, or — a subagent session has none — those of its parent
// session.
func (s SessionInfo) Owner() Owner {
	if s.AccountID != "" || s.OrganizationID != "" {
		return Owner{s.AccountID, s.OrganizationID}
	}
	return Owner{s.ParentAccountID, s.ParentOrganizationID}
}

// Owners lists the distinct effective owners of all sessions, sorted;
// sessions without any owner are the zero Owner. It is the universe that
// names and groups are resolved against for filters.
func (s *Store) Owners(ctx context.Context) ([]Owner, error) {
	acc, org := ownerExprs("")
	rows, err := s.db().QueryContext(ctx, `SELECT DISTINCT `+acc+`, `+org+` FROM sessions s`)
	if err != nil {
		return nil, fmt.Errorf("store: owners: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Owner
	for rows.Next() {
		var o Owner
		if err := rows.Scan(&o.Account, &o.Organization); err != nil {
			return nil, fmt.Errorf("store: owners: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: owners: %w", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Account != out[j].Account {
			return out[i].Account < out[j].Account
		}
		return out[i].Organization < out[j].Organization
	})
	return out, nil
}
