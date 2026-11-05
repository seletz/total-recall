// Command rawdict trains the zstd dictionary for events.raw
// (internal/store/rawz) from synthetic transcript lines and measures how
// well a dictionary compresses an archive. It is `mise run dict:train`.
//
//	go run ./tools/rawdict train [-samples DIR] [-out FILE] [-id N] [-size N]
//	go run ./tools/rawdict samples -out DIR
//	go run ./tools/rawdict measure -db FILE [-dict FILE] [-limit N]
//	go run ./tools/rawdict dump -db FILE -out DIR [-limit N]
//
// train builds the dictionary from the trimmed raw lines of the demo
// archive (internal/demo) plus the generated lines of synth.go, or from the
// files of -samples, and writes it to -out. samples writes those synthetic
// lines as one file per line. measure
// reads an archive (schema v4: raw as text, or v5: raw as zstd blob),
// trims the lines like the Claude adapter does, compresses them with and
// without a dictionary and prints sizes only, never content. dump writes
// the trimmed lines of an archive as sample files for a local comparison;
// never commit the output of dump or a dictionary trained from it: the
// repository holds only dictionaries trained from synthetic lines.
//
// The training input is the same on every run (samples shows it); the
// dictionary builder breaks ties in map order, so two runs give byte-wise
// different, equivalent dictionaries. A new dictionary that ships gets a
// new ID and is appended to rawz.Dicts; the old one stays for old rows.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/dict"
	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/harness/claude"
	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store/rawz"
)

const defaultOut = "internal/store/rawz/dict/raw-v1.zdict"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "train":
		err = train(os.Args[2:])
	case "samples":
		err = samples(os.Args[2:])
	case "measure":
		err = measure(os.Args[2:])
	case "dump":
		err = dump(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rawdict:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: rawdict train|samples|measure|dump [flags]")
	os.Exit(2)
}

// synthSamples is the number of generated lines (synth.go) that join the
// demo archive's lines as training input.
const synthSamples = 6000

// trainingLines returns the synthetic training input: the trimmed raw
// lines of the demo archive and the generated lines, sorted and
// deduplicated (deterministic input for the trainer).
func trainingLines() ([][]byte, error) {
	lines, err := demoLines()
	if err != nil {
		return nil, err
	}
	return append(lines, synthLines(synthSamples)...), nil
}

// demoLines returns the trimmed raw lines of the demo archive, one per
// event, deduplicated and sorted.
func demoLines() ([][]byte, error) {
	tmp, err := os.MkdirTemp("", "total-recall-rawdict-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	projects, err := demo.Write(tmp)
	if err != nil {
		return nil, err
	}
	a := claude.New(projects)
	srcs, err := a.Sources()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, p := range srcs {
		_, err := a.Parse(context.Background(), p, 0, func(r model.Record) error {
			for _, e := range r.Events {
				if e.Raw != "" {
					seen[e.Raw] = true
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	lines := make([]string, 0, len(seen))
	for l := range seen {
		lines = append(lines, l)
	}
	sort.Strings(lines)
	out := make([][]byte, len(lines))
	for i, l := range lines {
		out[i] = []byte(l)
	}
	return out, nil
}

func readSamples(dir string) ([][]byte, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func writeSamples(dir string, lines [][]byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i, l := range lines {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%06d.json", i)), l, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func train(args []string) error {
	fs := flag.NewFlagSet("train", flag.ExitOnError)
	samplesDir := fs.String("samples", "", "directory of sample files (default: the demo archive's lines)")
	out := fs.String("out", defaultOut, "dictionary file to write")
	id := fs.Uint("id", uint(rawz.DictIDV1), "dictionary ID")
	size := fs.Int("size", 112<<10, "maximum dictionary size in bytes")
	hash := fs.Int("hash", 6, "minimum match length the builder indexes (4-8)")
	_ = fs.Parse(args)
	var lines [][]byte
	var err error
	if *samplesDir != "" {
		lines, err = readSamples(*samplesDir)
	} else {
		lines, err = trainingLines()
	}
	if err != nil {
		return err
	}
	start := time.Now()
	d, err := dict.BuildZstdDict(lines, dict.Options{MaxDictSize: *size, HashBytes: *hash, ZstdDictID: uint32(*id), ZstdLevel: zstd.SpeedBetterCompression})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(*out, d, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "dictionary %s: %d bytes, id %#x, from %d lines, %s\n", *out, len(d), *id, len(lines), time.Since(start).Round(time.Millisecond))
	return nil
}

func samples(args []string) error {
	fs := flag.NewFlagSet("samples", flag.ExitOnError)
	out := fs.String("out", "", "directory to write one file per line into")
	_ = fs.Parse(args)
	if *out == "" {
		return fmt.Errorf("samples: -out is required")
	}
	lines, err := trainingLines()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d lines to %s\n", len(lines), *out)
	return writeSamples(*out, lines)
}

// rawLines calls fn with the trimmed raw line of every event of the
// archive at path that has one (text rows of schema v4 are trimmed, blobs
// of v5 are decoded first).
func rawLines(path string, limit int, fn func(orig, trimmed []byte) error) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	q := "SELECT raw FROM events WHERE raw IS NOT NULL ORDER BY seq"
	if limit > 0 {
		q += " LIMIT " + strconv.Itoa(limit)
	}
	rows, err := db.Query(q)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if rawz.IsFrame(raw) {
			s, err := rawz.Decode(raw)
			if err != nil {
				return err
			}
			raw = []byte(s)
		}
		trimmed, err := claude.TrimRaw(raw)
		if err != nil {
			trimmed = raw
		}
		if err := fn(raw, trimmed); err != nil {
			return err
		}
	}
	return rows.Err()
}

func measure(args []string) error {
	fs := flag.NewFlagSet("measure", flag.ExitOnError)
	dbPath := fs.String("db", "", "archive to measure")
	dictPath := fs.String("dict", "", "dictionary file (default: the embedded one)")
	limit := fs.Int("limit", 0, "only the first N events")
	_ = fs.Parse(args)
	if *dbPath == "" {
		return fmt.Errorf("measure: -db is required")
	}
	dicts := rawz.Dicts
	if *dictPath != "" {
		d, err := os.ReadFile(*dictPath)
		if err != nil {
			return err
		}
		dicts = [][]byte{d}
	}
	with, err := rawz.New(dicts...)
	if err != nil {
		return err
	}
	without, err := rawz.New()
	if err != nil {
		return err
	}
	var n, orig, trimmed, zWith, zWithout int64
	var tWith, tWithout, tDecode time.Duration
	start := time.Now()
	err = rawLines(*dbPath, *limit, func(o, t []byte) error {
		n++
		orig += int64(len(o))
		trimmed += int64(len(t))
		s := string(t)
		t0 := time.Now()
		b := with.Encode(s)
		tWith += time.Since(t0)
		zWith += int64(len(b))
		t0 = time.Now()
		if _, err := with.Decode(b); err != nil {
			return err
		}
		tDecode += time.Since(t0)
		t0 = time.Now()
		zWithout += int64(len(without.Encode(s)))
		tWithout += time.Since(t0)
		return nil
	})
	if err != nil {
		return err
	}
	pct := func(a, b int64) string {
		if b == 0 {
			return "-"
		}
		return fmt.Sprintf("%5.1f %%", 100*float64(a)/float64(b))
	}
	fmt.Printf("events with raw      %d\n", n)
	fmt.Printf("raw as stored        %s\n", mib(orig))
	fmt.Printf("trimmed              %s  (%s of stored)\n", mib(trimmed), pct(trimmed, orig))
	fmt.Printf("zstd, no dictionary  %s  (%s of trimmed, %s of stored)  %s\n", mib(zWithout), pct(zWithout, trimmed), pct(zWithout, orig), tWithout.Round(time.Millisecond))
	fmt.Printf("zstd, dictionary     %s  (%s of trimmed, %s of stored)  %s\n", mib(zWith), pct(zWith, trimmed), pct(zWith, orig), tWith.Round(time.Millisecond))
	fmt.Printf("decode               %s\n", tDecode.Round(time.Millisecond))
	fmt.Printf("total                %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}

func dump(args []string) error {
	fs := flag.NewFlagSet("dump", flag.ExitOnError)
	dbPath := fs.String("db", "", "archive to read")
	out := fs.String("out", "", "directory to write one file per line into (never commit it)")
	limit := fs.Int("limit", 0, "only the first N events")
	_ = fs.Parse(args)
	if *dbPath == "" || *out == "" {
		return fmt.Errorf("dump: -db and -out are required")
	}
	var lines [][]byte
	err := rawLines(*dbPath, *limit, func(_, t []byte) error {
		lines = append(lines, append([]byte(nil), t...))
		return nil
	})
	if err != nil {
		return err
	}
	return writeSamples(*out, lines)
}

func mib(n int64) string {
	s := fmt.Sprintf("%8.1f MiB", float64(n)/(1<<20))
	return strings.TrimLeft(s, " ")
}
