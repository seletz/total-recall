// Command demo builds the synthetic demo archive of total-recall as a
// database file; see internal/demo. It is `mise run demo:db`: the tests
// read the file through TOTAL_RECALL_DEMO_DB and the screenshots of the
// docs are rendered from it. With -pool it builds the demo pool instead,
// one archive per host plus the reference all.db (`mise run demo:pool`).
//
//	go run ./tools/demo -out build/demo.db
//	go run ./tools/demo -pool build/demo-pool
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
	pool := flag.String("pool", "", "build the demo pool into this directory instead (<host>.db per host and all.db); existing archives are replaced")
	flag.Parse()
	start := time.Now()
	if *pool != "" {
		p, err := demo.BuildPool(context.Background(), *pool)
		if err != nil {
			fmt.Fprintln(os.Stderr, "demo:", err)
			os.Exit(1)
		}
		for _, h := range p.Hosts {
			fmt.Fprintf(os.Stderr, "demo pool %s: %d sessions, %d events\n", h.Path, h.Sessions, h.Events)
		}
		fmt.Fprintf(os.Stderr, "demo pool %s: all hosts, %s\n", p.All, time.Since(start).Round(time.Millisecond))
		return
	}
	sum, err := demo.Build(context.Background(), *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "demo archive %s: %d sessions, %d events, %s to %s, %s\n",
		*out, sum.Sessions, sum.Events, demo.First.Format("2006-01-02"), demo.Last.Format("2006-01-02"), time.Since(start).Round(time.Millisecond))
}
