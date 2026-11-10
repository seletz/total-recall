package ingest

import (
	"context"
	"time"

	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
)

// ReclassifySummary describes a Reclassify run.
type ReclassifySummary struct {
	Examined    int            // stored user messages looked at
	Changed     int            // messages whose actor differs under the current rules (written unless DryRun)
	InputKinds  int            // messages whose input kind differs under the current rules (written unless DryRun)
	Dispatched  int            // messages now automation because another session sent the text (part of Changed)
	Undecided   int            // messages the harness could not decide (no text rule matched, raw record missing)
	Transitions map[string]int // "<old> -> <new>" → count of changed messages
	DryRun      bool
	Duration    time.Duration
}

// Reclassify applies the current actor rules of every adapter that
// implements harness.Reclassifier to the stored user messages of its
// harness and updates the actors that differ. With dryRun it only counts.
// It is idempotent: a second run with the same rules changes nothing.
func Reclassify(ctx context.Context, d Deps, dryRun bool) (ReclassifySummary, error) {
	start := time.Now()
	sum := ReclassifySummary{Transitions: map[string]int{}, DryRun: dryRun}
	ix, err := dispatchIndex(ctx, d, time.Time{}, time.Time{})
	if err != nil {
		return sum, err
	}
	for _, a := range d.Adapters {
		rc, ok := a.(harness.Reclassifier)
		if !ok {
			continue
		}
		var changes []store.ActorChange
		err := d.Store.UserMessages(ctx, a.Name(), func(m store.UserMessage) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			sum.Examined++
			actor, ok := rc.Reclassify(toEvent(m), m.Subagent)
			if !ok {
				sum.Undecided++
				actor = m.Actor // the actor stays; the input kind may still be decidable
			}
			// A human input another session sent is automation.
			if actor == model.ActorHuman && sentByAnother(ix, m) {
				actor = model.ActorAutomation
				if m.Actor != actor {
					sum.Dispatched++
				}
			}
			if actor != m.Actor {
				sum.Changed++
				sum.Transitions[string(m.Actor)+" -> "+string(actor)]++
			}
			// The input kind exists for human messages only.
			kind := m.InputKind
			if actor != model.ActorHuman {
				kind = ""
			} else if ik, isIK := a.(harness.InputKinder); isIK {
				if k, decided := ik.InputKind(toEvent(m)); decided {
					kind = k
				}
			}
			if kind != m.InputKind {
				sum.InputKinds++
			}
			if actor != m.Actor || kind != m.InputKind {
				changes = append(changes, store.ActorChange{ID: m.ID, Actor: actor, InputKind: kind})
			}
			return nil
		})
		if err != nil {
			return sum, err
		}
		if dryRun || len(changes) == 0 {
			continue
		}
		if _, err := d.Store.SetEventActors(ctx, changes); err != nil {
			return sum, err
		}
	}
	sum.Duration = time.Since(start)
	return sum, nil
}

func toEvent(m store.UserMessage) model.Event {
	return model.Event{ID: m.ID, Actor: m.Actor, Text: m.Text, Raw: m.Raw}
}
