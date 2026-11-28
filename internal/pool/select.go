package pool

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Skipped is a copy in the pool that does not count, with the reason.
type Skipped struct {
	// Host is the folder of the copy, Backup its path.
	Host, Backup, Reason string
}

// HostName is the host an archive counts for: the host it recorded, else
// the name of its folder.
func (a Archive) HostName() string { return cmp.Or(a.Archive.Host, a.Host) }

// Select picks the archives of the pool that count, each once, by host
// name, so a reader never counts an archive twice:
//
//   - an archive without identity (schema before v11) is left out: the
//     reader could not tell it from another copy of itself;
//   - of the copies of one archive (same ID, e.g. the old folder of a
//     renamed host) only the newest counts;
//   - two different archives that claim the same host are both left out:
//     their sessions could not be told apart by host.
func Select(archives []Archive) (keep []Archive, skipped []Skipped) {
	skip := func(a Archive, reason string) {
		skipped = append(skipped, Skipped{Host: a.Host, Backup: a.Entry.Path, Reason: reason})
	}
	byID := map[string][]Archive{}
	var ids []string
	for _, a := range archives {
		if a.Archive.ID == "" {
			skip(a, fmt.Sprintf("schema v%d has no identity: run migrate on %s, then backup", a.SchemaVersion, a.Host))
			continue
		}
		if _, ok := byID[a.Archive.ID]; !ok {
			ids = append(ids, a.Archive.ID)
		}
		byID[a.Archive.ID] = append(byID[a.Archive.ID], a)
	}
	byHost := map[string][]Archive{}
	for _, id := range ids {
		copies := byID[id]
		// Newest first; of equally new copies the one in its own folder.
		slices.SortStableFunc(copies, func(x, y Archive) int {
			return cmp.Or(y.At.Compare(x.At), boolRank(x.Host != x.HostName())-boolRank(y.Host != y.HostName()))
		})
		for _, c := range copies[1:] {
			skip(c, "another copy of the archive of "+copies[0].HostName()+", an older or equally new one")
		}
		byHost[copies[0].HostName()] = append(byHost[copies[0].HostName()], copies[0])
	}
	for _, list := range byHost {
		if len(list) == 1 {
			keep = append(keep, list[0])
			continue
		}
		var folders []string
		for _, a := range list {
			folders = append(folders, a.Host)
		}
		slices.Sort(folders)
		for _, a := range list {
			skip(a, fmt.Sprintf("two archives claim host %s (%s): set host on one of them", a.HostName(), strings.Join(folders, ", ")))
		}
	}
	slices.SortFunc(keep, func(x, y Archive) int { return strings.Compare(x.HostName(), y.HostName()) })
	slices.SortFunc(skipped, func(x, y Skipped) int { return strings.Compare(x.Host, y.Host) })
	return keep, skipped
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}
