// Package harness defines the adapter interface every agent harness implements.
package harness

import (
	"context"

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

// RawTrimmer is implemented by adapters that can reduce a transcript line
// to the fields they need to read it again (Parse, Reclassify). The adapter
// applies it when it emits an event, and the store applies it to stored raw
// lines when it migrates them (schema v5); the store itself does not know
// the format. The result is compact JSON; an input that is not a record is
// an error, and the caller then keeps the line as it is.
type RawTrimmer interface {
	TrimRaw(raw []byte) ([]byte, error)
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
