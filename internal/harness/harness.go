// Package harness defines the adapter interface every agent harness implements.
package harness

import (
	"context"
	"time"

	"github.com/seletz/total-recall/internal/model"
)

// Adapter reads one harness's transcript format.
type Adapter interface {
	// Name is the short harness name used in IDs, e.g. "claude".
	Name() string
	// Sources lists all transcript files the harness currently has on disk.
	Sources() ([]string, error)
	// Parse reads path starting at byte offset and calls emit for every complete line.
	// It returns the offset after the last complete line it consumed. A trailing
	// incomplete line is left for the next call.
	Parse(ctx context.Context, path string, offset int64, emit func(model.Record) error) (int64, error)
}

// Reclassifier is implemented by adapters that can derive the actor of a
// stored user message again, after the classification rules changed
// (ingest --reclassify). ev carries the stored text and raw record;
// subagent says whether the event's session is a subagent transcript. ok is
// false when the adapter cannot decide from what is stored; the stored actor
// then stays.
type Reclassifier interface {
	Reclassify(ev model.Event, subagent bool) (actor model.Actor, ok bool)
}

// InputKinder is implemented by adapters that can derive how a stored human
// message reached the harness (model.InputKind) from its text and raw
// record (ingest --reclassify). ok is false when it cannot be decided from
// what is stored; the stored kind then stays.
type InputKinder interface {
	InputKind(ev model.Event) (kind model.InputKind, ok bool)
}

var registry = map[string]Adapter{}

// Register adds an adapter; called from adapter packages' init or from main.
func Register(a Adapter) { registry[a.Name()] = a }

// Get returns the adapter for name.
func Get(name string) (Adapter, bool) { a, ok := registry[name]; return a, ok }

// All returns all registered adapters.
func All() []Adapter {
	out := make([]Adapter, 0, len(registry))
	for _, a := range registry {
		out = append(out, a)
	}
	return out
}

// MemorySource is implemented by adapters whose harness keeps a memory
// per project outside the transcripts (Claude Code: memory/*.md in each
// project directory). Ingest archives it versioned (store.WriteMemory);
// the store knows only the project key and paths relative to the memory
// directory, the adapter knows where the files are.
type MemorySource interface {
	// Memory lists the memory files of every project that has a memory
	// directory. ok is false when the harness's data directory does not
	// exist at all: then nothing is known, and ingest records no deletions.
	Memory() (projects []MemoryProject, ok bool, err error)
	// MemoryCwd returns the working directory of the project with key:
	// from the harness's own data (a transcript of the project), else the
	// first of candidates (working directories of archived sessions) that
	// maps to key; "" when neither tells.
	MemoryCwd(key string, candidates []string) string
}

// MemoryProject is the memory directory of one project.
type MemoryProject struct {
	// Key is the harness's native project key, stable across runs.
	Key string
	// Files are the memory files, by path.
	Files []MemoryFile
}

// MemoryFile is one file of a project's memory.
type MemoryFile struct {
	// Path is relative to the memory directory, slash-separated.
	Path string
	// Abs is the absolute path to read the file from.
	Abs   string
	Size  int64
	MTime time.Time
}
