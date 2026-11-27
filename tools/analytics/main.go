// Command analytics runs the ETL of spike #349 from an archive into an
// analytics database (internal/analytics). Only for the spike: the demo
// archive or a copy, never the live archive.
//
//	go run ./tools/analytics -archive build/demo.db -out build/analytics.db
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/seletz/total-recall/internal/analytics"
	"github.com/seletz/total-recall/internal/demo"
)

func main() {
	archive := flag.String("archive", "build/demo.db", "source archive (opened read-only)")
	out := flag.String("out", "build/analytics.db", "analytics database")
	source := flag.String("source", "demo", "name of the source archive for the watermark")
	host := flag.String("host", "host-a", "host when the archive has no sessions.host")
	tz := flag.String("tz", "Europe/Berlin", "primary time zone")
	home := flag.String("home", demo.Home, "home directory shown as ~")
	full := flag.Bool("full", false, "reload all facts")
	flag.Parse()
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	r, err := analytics.Run(context.Background(), analytics.Options{
		Archive: *archive, Target: *out, Source: *source, Host: *host, TZ: loc, Home: *home, Full: *full,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("batch %d %s: seq (%d, %d], %d events in %d sessions staged, %s\n",
		r.BatchID, r.Mode, r.SeqFrom, r.SeqTo, r.Events, r.Sessions, r.Elapsed.Round(time.Millisecond))
}
