// Command demo builds the synthetic demo archive of total-recall as a
// database file; see internal/demo. It is `mise run demo:db`: the tests
// read the file through TOTAL_RECALL_DEMO_DB and the screenshots of the
// docs are rendered from it.
//
//	go run ./tools/demo -out build/demo.db
//
// The archive is invented and deterministic; it is safe to look at with
// `total-recall --db build/demo.db tui`.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/seletz/total-recall/internal/demo"
)

func main() {
	out := flag.String("out", "build/demo.db", "path of the archive to write; an existing file is replaced")
	flag.Parse()
	start := time.Now()
	sum, err := demo.Build(context.Background(), *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "demo archive %s: %d sessions, %d events, %s to %s, %s\n",
		*out, sum.Sessions, sum.Events, demo.First.Format("2006-01-02"), demo.Last.Format("2006-01-02"), time.Since(start).Round(time.Millisecond))
}
