package main

import (
	"context"
	"os"
	"testing"

	"github.com/seletz/total-recall/internal/demo"
)

// TestRun projects a demo pool archive into the PostgreSQL database named
// by TOTAL_RECALL_PG_DSN (a throwaway container, superuser; CI would run
// one as a service). Skipped without it.
func TestRun(t *testing.T) {
	dsn := os.Getenv("TOTAL_RECALL_PG_DSN")
	if dsn == "" {
		t.Skip("TOTAL_RECALL_PG_DSN not set")
	}
	dir := t.TempDir()
	p, err := demo.BuildPool(context.Background(), dir, []string{"host-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), p.All, dsn); err != nil {
		t.Fatal(err)
	}
}
