// Command sbom generates and checks the CycloneDX SBOMs of a release; the
// logic is in internal/sbom.
//
// goreleaser calls `generate` once per built binary (.goreleaser.yml, section
// sboms): it runs the pinned cyclonedx-gomod against the binary, names the
// main component after the project and checks the document before goreleaser
// goes on to checksums and upload.
//
//	go run ./tools/sbom generate -binary dist/total-recall_linux_amd64_v1/total-recall \
//	    -out dist/total-recall-v1.0.0-linux-amd64.cdx.json -goos linux -goarch amd64 -version 1.0.0
//
// `check` verifies a finished dist directory (CI on every pull request,
// `mise run sbom:check`) or single SBOM files, for example ones downloaded
// from a release:
//
//	go run ./tools/sbom check dist
//	go run ./tools/sbom check -version 1.0.0 total-recall-v1.0.0-darwin-arm64.cdx.json
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/seletz/total-recall/internal/sbom"
)

const (
	defaultName = "total-recall"
	defaultSpec = "1.6"
	defaultTool = "cyclonedx-gomod"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, sbom.ErrUsage) {
			fmt.Fprintln(os.Stderr, "usage: sbom generate|check [flags] ...; see -h of each")
		} else {
			fmt.Fprintln(os.Stderr, "sbom:", err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return sbom.ErrUsage
	}
	switch args[0] {
	case "generate":
		return generate(args[1:])
	case "check":
		return check(args[1:])
	default:
		return fmt.Errorf("%w: unknown command %q", sbom.ErrUsage, args[0])
	}
}

// generate runs cyclonedx-gomod against one binary, finishes the document and
// checks it.
func generate(args []string) error {
	fs := flag.NewFlagSet("sbom generate", flag.ContinueOnError)
	var o sbom.GenerateOptions
	fs.StringVar(&o.Tool, "tool", defaultTool, "cyclonedx-gomod executable")
	fs.StringVar(&o.Binary, "binary", "", "built binary to catalog")
	fs.StringVar(&o.Output, "out", "", "SBOM file to write, named <name>-<tag>-<os>-<arch>"+sbom.Suffix)
	fs.StringVar(&o.GOOS, "goos", "", "GOOS of the binary")
	fs.StringVar(&o.GOARCH, "goarch", "", "GOARCH of the binary")
	fs.StringVar(&o.Version, "version", "", "release version without the leading v")
	fs.StringVar(&o.SpecVersion, "spec", defaultSpec, "CycloneDX spec version")
	name := fs.String("name", defaultName, "project name: main component and file name prefix")
	sumPath := fs.String("sum", "go.sum", "go.sum to check the module hashes against")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if o.Binary == "" || o.Output == "" || o.GOOS == "" || o.GOARCH == "" || o.Version == "" || fs.NArg() != 0 {
		return fmt.Errorf("%w: generate needs -binary, -out, -goos, -goarch and -version", sbom.ErrUsage)
	}
	sum, err := readGoSum(*sumPath)
	if err != nil {
		return err
	}
	tool, toolArgs, env := sbom.Command(o)
	cmd := exec.Command(tool, toolArgs...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", tool, err)
	}
	if err := sbom.Finish(o.Output, *name, o.Version); err != nil {
		return err
	}
	rep, err := sbom.CheckFile(o.Output, sbom.Expect{Name: *name, Version: o.Version, SpecVersion: o.SpecVersion}, sum)
	if err != nil {
		return err
	}
	fmt.Println(rep)
	return nil
}

// check verifies a goreleaser dist directory or single SBOM files.
func check(args []string) error {
	fs := flag.NewFlagSet("sbom check", flag.ContinueOnError)
	name := fs.String("name", defaultName, "project name (files only; a dist directory carries it in metadata.json)")
	version := fs.String("version", "", "release version without the leading v (files only)")
	spec := fs.String("spec", defaultSpec, "CycloneDX spec version")
	sumPath := fs.String("sum", "go.sum", "go.sum to check the module hashes against")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("%w: check needs a dist directory or SBOM files", sbom.ErrUsage)
	}
	sum, err := readGoSum(*sumPath)
	if err != nil {
		return err
	}
	for _, path := range fs.Args() {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			reports, err := sbom.CheckDist(path, sum, *spec)
			if err != nil {
				return err
			}
			for _, rep := range reports {
				fmt.Println(rep)
			}
			continue
		}
		if *version == "" {
			return fmt.Errorf("%w: checking files needs -version", sbom.ErrUsage)
		}
		rep, err := sbom.CheckFile(path, sbom.Expect{Name: *name, Version: *version, SpecVersion: *spec}, sum)
		if err != nil {
			return err
		}
		fmt.Println(rep)
	}
	return nil
}

func readGoSum(path string) (sbom.GoSum, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum, err := sbom.ParseGoSum(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return sum, nil
}
