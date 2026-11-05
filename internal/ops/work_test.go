package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/config"
)

func TestWorkHours(t *testing.T) {
	tests := []struct {
		name    string
		hours   string
		wantErr string
	}{
		{"defaults", "09:00-17:00", ""},
		{"bad hours", "nine to five", "work.hours"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Load(config.Options{GOOS: "linux", Home: t.TempDir(), Getenv: func(string) string { return "" }})
			if err != nil {
				t.Fatal(err)
			}
			cfg.Work.Hours = tt.hours
			w, err := WorkHours(cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("WorkHours error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if w.Start != 9*time.Hour || w.End != 17*time.Hour || w.Target != 8*time.Hour || w.Low != 6*time.Hour || w.High != 9*time.Hour || w.WeeklyLow != 32*time.Hour || w.WeeklyHigh != 48*time.Hour {
				t.Errorf("WorkHours = %+v", w)
			}
			if got := Holidays(cfg).Region(); got != "Baden-Württemberg" {
				t.Errorf("Holidays region = %q", got)
			}
		})
	}
}
