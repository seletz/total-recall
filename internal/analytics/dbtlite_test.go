package analytics

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

func TestRenderModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
		wantErr        bool
	}{
		{"config dropped", "{{ config(materialized='table', contract={'enforced': true}) }}\nSELECT 1", "\nSELECT 1", false},
		{"source", "FROM {{ source('tr', 'fact_event') }} f", "FROM fact_event f", false},
		{"source double quotes", `FROM {{source("tr","dim_host")}}`, "FROM dim_host", false},
		{"var", `b.tz = '{{ var("tz") }}'`, "b.tz = 'Europe/Berlin'", false},
		{"unknown var", `{{ var("nope") }}`, "", true},
		{"other jinja is refused", "{% if x %}a{% endif %}", "", true},
		{"ref is refused", "{{ ref('daily') }}", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := RenderModel(tt.in, map[string]string{"tz": "Europe/Berlin"})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDbtModelInSQLite runs the dbt model of tools/analytics/dbt, rendered by
// RenderModel, in the SQLite target and compares it with mart_daily: the
// same file gives the same rows in SQLite (here) and in PostgreSQL (dbt,
// scripts/analytics-pg.sh).
func TestDbtModelInSQLite(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("..", "..", "tools", "analytics", "dbt", "models", "daily.sql"))
	if err != nil {
		t.Fatal(err)
	}
	model, err := RenderModel(string(b), map[string]string{"tz": "Europe/Berlin"})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "analytics.db")
	mustRun(t, opts(t, os.Getenv(fixturedb.DemoEnv), target))
	const cols = "day_local, host, project, sessions, human_prompts, agent_events, tool_calls, tokens_in, tokens_out"
	diff := scalarRow(t, target, "SELECT (SELECT COUNT(*) FROM (SELECT "+cols+" FROM ("+model+") EXCEPT SELECT "+cols+" FROM analytics_daily_v1)), (SELECT COUNT(*) FROM ("+model+")), (SELECT COUNT(*) FROM analytics_daily_v1)")
	if diff != "0|304|304" {
		t.Errorf("model vs daily_v1 (differing rows|model rows|view rows) = %s, want 0|304|304", diff)
	}
}
