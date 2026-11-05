package ops

import (
	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/holiday"
	"github.com/seletz/total-recall/internal/store"
)

// WorkHours is the [work] table of cfg as the store rates days with it:
// the core hours as offsets from midnight, the daily target and the daily
// and weekly bounds. The error is that of config.WorkHours (a bad value,
// core hours before day_start).
func WorkHours(cfg *config.Config) (store.WorkHours, error) {
	start, end, err := cfg.WorkHours()
	if err != nil {
		return store.WorkHours{}, err
	}
	return store.WorkHours{
		Start: start, End: end, Target: cfg.Work.Target.Duration,
		Low: cfg.Work.DailyLow.Duration, High: cfg.Work.DailyHigh.Duration,
		WeeklyLow: cfg.Work.WeeklyLow.Duration, WeeklyHigh: cfg.Work.WeeklyHigh.Duration,
	}, nil
}

// Holidays is the holiday calendar of the configured Bundesland.
func Holidays(cfg *config.Config) *holiday.Calendar { return holiday.New(cfg.Work.Bundesland) }
