//go:build realdata

// Sanity check against the local transcripts. Never run in CI; prints aggregate
// counts only (no content):
//
//	go test -tags realdata -run TestRealData -v ./internal/harness/claude/
package claude

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/seletz/total-recall/internal/model"
)

func TestRealData(t *testing.T) {
	a := New("")
	srcs, err := a.Sources()
	if err != nil {
		t.Fatal(err)
	}
	kindActor := map[string]int{}
	humanPerMonth := map[string]int{}
	sessions := map[string]bool{}
	subagents := map[string]bool{}
	ids := map[string]bool{}
	dupIDs, events, parseFailures := 0, 0, 0
	var tokIn, tokOut int64
	for _, p := range srcs {
		_, err := a.Parse(context.Background(), p, 0, func(r model.Record) error {
			sessions[r.Session.ID] = true
			if r.Session.ParentID != "" {
				subagents[r.Session.ID] = true
			}
			for _, e := range r.Events {
				events++
				if ids[e.ID] {
					dupIDs++
				}
				ids[e.ID] = true
				kindActor[fmt.Sprintf("%-11s %-10s", e.Kind, e.Actor)]++
				if e.Actor == model.ActorHuman {
					humanPerMonth[e.TS.Format("2006-01")]++
				}
				tokIn += e.TokensIn
				tokOut += e.TokensOut
			}
			return nil
		})
		if err != nil {
			parseFailures++
			t.Logf("parse error in a source: %v", err)
		}
	}
	st := a.Stats()
	t.Logf("sources=%d sessions=%d (subagent=%d) lines=%d malformed=%d parseFailures=%d events=%d duplicateEventIDs=%d tokensIn=%d tokensOut=%d",
		len(srcs), len(sessions), len(subagents), st.Lines, st.Malformed, parseFailures, events, dupIDs, tokIn, tokOut)
	logSorted(t, "records per type", st.Records)
	logSorted(t, "events per kind/actor", toInt64(kindActor))
	logSorted(t, "human messages per month (UTC)", toInt64(humanPerMonth))
}

func toInt64(m map[string]int) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		out[k] = int64(v)
	}
	return out
}

func logSorted(t *testing.T, title string, m map[string]int64) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("%s:", title)
	for _, k := range keys {
		t.Logf("  %-24s %8d", k, m[k])
	}
}
