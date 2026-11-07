package memview

import (
	"slices"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/store"
)

// EventKind is what happened to a file at one version.
type EventKind int

// Kinds of an Event.
const (
	// Added is the first version of a file, or a version after a deletion:
	// the file is new on the disk.
	Added EventKind = iota
	// Changed is a version that replaces another one's content.
	Changed
	// Deleted is the scan that found the file gone.
	Deleted
)

func (k EventKind) String() string {
	switch k {
	case Added:
		return "added"
	case Deleted:
		return "deleted"
	}
	return "changed"
}

// Event is one version of one file seen as a change of the project's
// memory.
type Event struct {
	Path    string
	Kind    EventKind
	Version store.MemoryVersion
	// Prev and PrevSHA are the time and the content hash of the version
	// before, for a Changed event; Delta is the change in size.
	Prev    time.Time
	PrevSHA string
	Delta   int64
}

// Timeline turns the versions of the files of a project (oldest first, as
// the store returns them, by path) into the events of the project, newest
// first; changes at the same time are ordered by their sequence.
func Timeline(versions map[string][]store.MemoryVersion) []Event {
	var out []Event
	for path, vs := range versions {
		var prev *store.MemoryVersion
		for i := range vs {
			v := vs[i]
			e := Event{Path: path, Version: v}
			switch {
			case v.Deleted:
				e.Kind = Deleted
				if prev != nil {
					e.Delta = -prev.Bytes
				}
			case prev == nil || prev.Deleted:
				e.Kind, e.Delta = Added, v.Bytes
			default:
				e.Kind, e.Delta = Changed, v.Bytes-prev.Bytes
				e.Prev, e.PrevSHA = prev.TS, prev.SHA256
			}
			out = append(out, e)
			prev = &vs[i]
		}
	}
	slices.SortFunc(out, func(a, b Event) int {
		if c := b.Version.TS.Compare(a.Version.TS); c != 0 {
			return c
		}
		if a.Version.Seq != b.Version.Seq {
			if a.Version.Seq > b.Version.Seq {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out
}

// glyph is the mark of a kind in the timeline: + added, ~ changed,
// ✗ deleted.
func (k EventKind) glyph() string {
	switch k {
	case Added:
		return "+"
	case Deleted:
		return "✗"
	}
	return "~"
}

// eventRow renders a change of the timeline: time, the glyph of the kind
// and the path, the change in size on the right.
func (m Model) eventRow(i, w int) string {
	e := m.events[i]
	label := e.Version.TS.In(m.loc).Format("2006-01-02 15:04") + " " + e.Kind.glyph() + " " + e.Path
	var note string
	switch {
	case e.Kind == Deleted:
		note = "−" + formatSize(-e.Delta)
	case e.Delta >= 0:
		note = "+" + formatSize(e.Delta)
	default:
		note = "−" + formatSize(-e.Delta)
	}
	return m.row(label, note, i == m.tcursor, e.Kind == Deleted, w)
}
