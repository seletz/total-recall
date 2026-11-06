package store

import (
	"testing"
	"time"
)

func TestTokenStats(t *testing.T) {
	s, _ := openTestStore(t)
	sess, evs := sampleBatch()
	// e4: 10 in (6 read, 3 write, 1 fresh), 20 out. e5: 100 in (90 read, 0 write), 5 out, other model.
	// e2 gets counts only through SQL below (a row archived without raw: no split).
	evs[4].TokensIn, evs[4].TokensCacheRead, evs[4].TokensCacheWrite, evs[4].TokensOut, evs[4].Model = 100, 90, 0, 5, "m2"
	evs[4].TS = ts("2026-11-01T01:00:00Z") // the next local day
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/a.jsonl", Harness: "claude"}, sess, evs); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db().ExecContext(ctx, "UPDATE events SET tokens_in = 50, tokens_out = 2, model = 'm1' WHERE id = 'claude:e2'"); err != nil {
		t.Fatal(err)
	}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	got, err := s.TokenStats(ctx, StatsFilter{Location: berlin})
	if err != nil {
		t.Fatal(err)
	}
	// e4 = 10/6/3/20 (m1), e5 = 100/90/0/5 (m2), e2 = 50 unsplit/2 (m1).
	want := Tokens{Events: 3, Input: 160, Fresh: 11, CacheRead: 96, CacheWrite: 3, Unsplit: 50, Output: 27}
	if got.Total != want {
		t.Errorf("Total = %+v, want %+v", got.Total, want)
	}
	if got.Total.Input != got.Total.Fresh+got.Total.CacheRead+got.Total.CacheWrite+got.Total.Unsplit {
		t.Errorf("parts do not add up: %+v", got.Total)
	}
	if len(got.ByModel) != 2 || got.ByModel[0].Model != "m2" || got.ByModel[0].Input != 100 || got.ByModel[1].Model != "m1" || got.ByModel[1].Input != 60 {
		t.Errorf("ByModel = %+v (largest input first)", got.ByModel)
	}
	if len(got.ByDay) != 2 || got.ByDay[0].Day != "2026-10-31" || got.ByDay[0].Input != 60 || got.ByDay[1].Day != "2026-11-01" || got.ByDay[1].Input != 100 {
		t.Errorf("ByDay = %+v", got.ByDay)
	}
	// The range filters like Stats does.
	got, err = s.TokenStats(ctx, StatsFilter{Since: ts("2026-11-01T00:00:00Z"), Location: berlin})
	if err != nil || got.Total.Events != 1 || got.Total.Input != 100 {
		t.Errorf("filtered = %+v, %v", got.Total, err)
	}
	// Empty range: zero value, no nil-map trouble.
	got, err = s.TokenStats(ctx, StatsFilter{Since: ts("2030-01-01T00:00:00Z")})
	if err != nil || got.Total != (Tokens{}) || len(got.ByDay) != 0 {
		t.Errorf("empty = %+v, %v", got, err)
	}
}
