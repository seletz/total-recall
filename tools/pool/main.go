// Command pool builds the demo agent pool of spike #347 (see
// demo.BuildPool): the synthetic demo transcripts split by project over
// hosts, one local archive per host plus the reference archive all.db.
//
//	go run ./tools/pool -dir build/pool -hosts host-a,host-b
//
// Then deliver the host archives to a central one with `total-recall
// receive` and `total-recall push` (scripts/pool-demo.sh does all of it).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/seletz/total-recall/internal/demo"
)

func main() {
	dir := flag.String("dir", "build/pool", "directory for the archives; existing ones are replaced")
	hosts := flag.String("hosts", "host-a,host-b", "comma-separated host names")
	flag.Parse()
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "pool:", err)
		os.Exit(1)
	}
	p, err := demo.BuildPool(context.Background(), *dir, strings.Split(*hosts, ","))
	if err != nil {
		fmt.Fprintln(os.Stderr, "pool:", err)
		os.Exit(1)
	}
	fmt.Printf("reference %s\n", p.All)
	names := make([]string, 0, len(p.Hosts))
	for h := range p.Hosts {
		names = append(names, h)
	}
	sort.Strings(names)
	for _, h := range names {
		fmt.Printf("%s %s projects=%s\n", h, p.Hosts[h], strings.Join(p.Projects[h], ","))
	}
}
