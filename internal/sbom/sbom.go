// Package sbom finishes and checks the CycloneDX SBOMs that the release
// ships next to its archives.
//
// goreleaser runs `cyclonedx-gomod bin` against every released binary
// (.goreleaser.yml, section sboms, through tools/sbom). The tool names the
// main component after the module path with a "v" version; Finish renames it
// to the project and strips the "v". CheckFile verifies one document against
// the file name schema, the CycloneDX header, the expected main component and
// go.sum; CheckDist verifies a whole goreleaser dist directory: one SBOM per
// archive, every SBOM in the checksum file with a matching digest.
//
// The package uses only the standard library; a release must not depend on
// the modules it documents.
package sbom

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Suffix is the file extension of a CycloneDX JSON document as released.
const Suffix = ".cdx.json"

// archiveSuffix is the file extension of the release archives.
const archiveSuffix = ".tar.gz"

// stdModule is the name cyclonedx-gomod gives the Go standard library.
const stdModule = "std"

// GoSum holds the module hashes of a go.sum file: SHA-256 digests in hex,
// keyed by module path and version. The "/go.mod" lines describe the module
// graph, not the module content, and are left out.
type GoSum map[[2]string]string

// ParseGoSum reads a go.sum file.
func ParseGoSum(r io.Reader) (GoSum, error) {
	sum := GoSum{}
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 3 {
			return nil, fmt.Errorf("go.sum line %d: want 3 fields, got %d", line, len(fields))
		}
		module, version, hash := fields[0], fields[1], fields[2]
		digest, ok := strings.CutPrefix(hash, "h1:")
		if !ok {
			return nil, fmt.Errorf("go.sum line %d: hash %q is not an h1 hash", line, hash)
		}
		raw, err := base64.StdEncoding.DecodeString(digest)
		if err != nil {
			return nil, fmt.Errorf("go.sum line %d: %w", line, err)
		}
		if len(raw) != sha256.Size {
			return nil, fmt.Errorf("go.sum line %d: digest has %d bytes, want %d", line, len(raw), sha256.Size)
		}
		if strings.HasSuffix(version, "/go.mod") {
			continue
		}
		sum[[2]string{module, version}] = hex.EncodeToString(raw)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return sum, nil
}

// Hash returns the hex SHA-256 digest go.sum records for a module version.
func (s GoSum) Hash(module, version string) (string, bool) {
	h, ok := s[[2]string{module, version}]
	return h, ok
}

// Asset is a release file name of the form <name>-<tag>-<os>-<arch><ext>,
// the schema of the archives and SBOMs.
type Asset struct {
	Name string
	Tag  string
	OS   string
	Arch string
	Ext  string
}

var assetRE = regexp.MustCompile(`^([a-z0-9-]+?)-(v[0-9][^/]*?)-([a-z0-9]+)-([a-z0-9]+)(\.cdx\.json|\.tar\.gz)$`)

// ParseAsset splits a release file name into its parts.
func ParseAsset(filename string) (Asset, error) {
	m := assetRE.FindStringSubmatch(filename)
	if m == nil {
		return Asset{}, fmt.Errorf("%s: file name does not match <name>-<tag>-<os>-<arch>%s", filename, Suffix)
	}
	return Asset{Name: m[1], Tag: m[2], OS: m[3], Arch: m[4], Ext: m[5]}, nil
}

// deprecatedLicensePrefix marks SPDX ids that cyclonedx-gomod's license
// detection emits for deprecated list entries (for example GPL-2.0+). They
// are not valid values for license.id, so Finish moves them to license.name.
const deprecatedLicensePrefix = "deprecated_"

// Finish sets name and version of the main component (metadata.component)
// of the SBOM at path and writes the file back. Detected licenses with a
// deprecated SPDX id are kept as free-text names so the document validates
// against the CycloneDX schema. Everything else, the purl of the main
// component included, stays as the tool wrote it.
func Finish(path, name, version string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%s: not a JSON object: %w", filepath.Base(path), err)
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(doc["metadata"], &meta); err != nil || meta == nil {
		return fmt.Errorf("%s: metadata missing", filepath.Base(path))
	}
	var component map[string]json.RawMessage
	if err := json.Unmarshal(meta["component"], &component); err != nil || component == nil {
		return fmt.Errorf("%s: main component (metadata.component) missing", filepath.Base(path))
	}
	component["name"] = mustJSON(name)
	component["version"] = mustJSON(version)
	meta["component"] = mustJSON(component)
	doc["metadata"] = mustJSON(meta)
	if raw, ok := doc["components"]; ok {
		fixed, err := fixLicenses(raw)
		if err != nil {
			return fmt.Errorf("%s: components: %w", filepath.Base(path), err)
		}
		doc["components"] = fixed
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// fixLicenses rewrites evidence.licenses[].license of every component: a
// deprecated SPDX id becomes the license name.
func fixLicenses(raw json.RawMessage) (json.RawMessage, error) {
	var components []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &components); err != nil {
		return nil, err
	}
	for _, c := range components {
		ev, ok := c["evidence"]
		if !ok {
			continue
		}
		var evidence map[string]json.RawMessage
		if err := json.Unmarshal(ev, &evidence); err != nil {
			return nil, err
		}
		var licenses []map[string]json.RawMessage
		if err := json.Unmarshal(evidence["licenses"], &licenses); err != nil {
			return nil, err
		}
		for _, l := range licenses {
			var license map[string]json.RawMessage
			if err := json.Unmarshal(l["license"], &license); err != nil {
				return nil, err
			}
			var id string
			if err := json.Unmarshal(license["id"], &id); err != nil {
				continue
			}
			if name, ok := strings.CutPrefix(id, deprecatedLicensePrefix); ok {
				delete(license, "id")
				license["name"] = mustJSON(name)
				l["license"] = mustJSON(license)
			}
		}
		evidence["licenses"] = mustJSON(licenses)
		c["evidence"] = mustJSON(evidence)
	}
	return mustJSON(components), nil
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// Expect is what a checked SBOM has to match.
type Expect struct {
	// Name of the project: the main component and the file name carry it.
	Name string
	// Version of the main component, without the leading v; its purl has
	// the version with the v.
	Version string
	// Tag the file name has to carry, for example "v1.0.0". Empty skips
	// the comparison: a snapshot names its files after the last tag but
	// carries a snapshot version.
	Tag string
	// SpecVersion of CycloneDX, for example "1.6".
	SpecVersion string
}

// Report summarizes one checked SBOM.
type Report struct {
	File    string
	OS      string
	Arch    string
	Modules int
	Go      string
}

// String formats the report as one line for the terminal.
func (r Report) String() string {
	return fmt.Sprintf("%s: %s/%s, %d modules, %s", r.File, r.OS, r.Arch, r.Modules, r.Go)
}

// document is the part of a CycloneDX document the checks read.
type document struct {
	BOMFormat   string `json:"bomFormat"`
	SpecVersion string `json:"specVersion"`
	Metadata    struct {
		Component *component `json:"component"`
	} `json:"metadata"`
	Components []component `json:"components"`
}

type component struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	PURL    string `json:"purl"`
	Hashes  []struct {
		Alg     string `json:"alg"`
		Content string `json:"content"`
	} `json:"hashes"`
	ExternalReferences []struct {
		URL  string `json:"url"`
		Type string `json:"type"`
	} `json:"externalReferences"`
}

// vcsURL returns the URL of the component's vcs reference, or "" if none.
func (c component) vcsURL() string {
	for _, r := range c.ExternalReferences {
		if r.Type == "vcs" {
			return r.URL
		}
	}
	return ""
}

// sha256Hex returns the SHA-256 hash the component lists, or "" if none.
func (c component) sha256Hex() string {
	for _, h := range c.Hashes {
		if h.Alg == "SHA-256" {
			return h.Content
		}
	}
	return ""
}

// CheckFile verifies one SBOM against the file name schema, the CycloneDX
// header, the expected main component and the module hashes in go.sum. The
// error names the file and the first finding.
func CheckFile(path string, e Expect, sum GoSum) (Report, error) {
	file := filepath.Base(path)
	fail := func(format string, args ...any) (Report, error) {
		return Report{}, fmt.Errorf("%s: %s", file, fmt.Sprintf(format, args...))
	}
	asset, err := ParseAsset(file)
	if err != nil {
		return Report{}, err
	}
	if asset.Ext != Suffix || asset.Name != e.Name {
		return fail("file name does not match %s-<tag>-<os>-<arch>%s", e.Name, Suffix)
	}
	if e.Tag != "" && asset.Tag != e.Tag {
		return fail("file name carries tag %s, want %s", asset.Tag, e.Tag)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return fail("invalid JSON: %v", err)
	}
	if doc.BOMFormat != "CycloneDX" {
		return fail("bomFormat is %q, want CycloneDX", doc.BOMFormat)
	}
	if doc.SpecVersion != e.SpecVersion {
		return fail("specVersion is %q, want %s", doc.SpecVersion, e.SpecVersion)
	}
	main := doc.Metadata.Component
	if main == nil {
		return fail("main component (metadata.component) missing")
	}
	if main.Name != e.Name || main.Version != e.Version {
		return fail("main component is %s %s, want %s %s", main.Name, main.Version, e.Name, e.Version)
	}
	if err := checkPURL(main.PURL, "v"+e.Version, asset); err != nil {
		return fail("main component: %v", err)
	}
	// The tool derives the vcs reference from the module path; the two have
	// to agree, or the SBOM points readers to the wrong repository.
	if want := "https://" + purlModule(main.PURL); main.vcsURL() != want {
		return fail("main component: vcs reference is %q, want %s", main.vcsURL(), want)
	}
	if len(doc.Components) == 0 {
		return fail("no components")
	}
	rep := Report{File: file, OS: asset.OS, Arch: asset.Arch}
	for _, c := range doc.Components {
		if err := checkPURL(c.PURL, c.Version, asset); err != nil {
			return fail("module %s: %v", c.Name, err)
		}
		if c.Name == stdModule {
			rep.Go = c.Version
			continue
		}
		want, ok := sum.Hash(c.Name, c.Version)
		if !ok {
			return fail("module %s %s is not in go.sum", c.Name, c.Version)
		}
		got := c.sha256Hex()
		if got == "" {
			return fail("module %s %s has no SHA-256 hash", c.Name, c.Version)
		}
		if got != want {
			return fail("module %s %s: hash %s differs from go.sum (%s)", c.Name, c.Version, got, want)
		}
		rep.Modules++
	}
	if rep.Go == "" {
		return fail("Go standard library (%s) missing", stdModule)
	}
	return rep, nil
}

// purlModule returns the module path of a Go package URL.
func purlModule(purl string) string {
	rest, _ := strings.CutPrefix(purl, "pkg:golang/")
	rest, _, _ = strings.Cut(rest, "#")
	rest, _, _ = strings.Cut(rest, "?")
	module, _, _ := strings.Cut(rest, "@")
	return module
}

// checkPURL verifies version and platform qualifiers of a Go package URL,
// for example pkg:golang/example.com/m@v1.0.0?goarch=arm64&goos=darwin&type=module#cmd/m.
func checkPURL(purl, version string, asset Asset) error {
	rest, ok := strings.CutPrefix(purl, "pkg:golang/")
	if !ok {
		return fmt.Errorf("purl %q is not a Go purl", purl)
	}
	rest, _, _ = strings.Cut(rest, "#")
	rest, query, _ := strings.Cut(rest, "?")
	_, got, ok := strings.Cut(rest, "@")
	if !ok || got != version {
		return fmt.Errorf("purl %q carries version %q, want %s", purl, got, version)
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return fmt.Errorf("purl %q: %w", purl, err)
	}
	if q.Get("goos") != asset.OS || q.Get("goarch") != asset.Arch {
		return fmt.Errorf("purl %q names platform %s/%s, file name %s/%s", purl, q.Get("goos"), q.Get("goarch"), asset.OS, asset.Arch)
	}
	return nil
}

// metadata is the part of goreleaser's dist/metadata.json the checks read.
type metadata struct {
	ProjectName string `json:"project_name"`
	Tag         string `json:"tag"`
	Version     string `json:"version"`
}

// CheckDist verifies the SBOMs of a goreleaser dist directory: project, tag
// and version come from metadata.json; every archive has an SBOM and every
// SBOM an archive; the single checksums file lists every SBOM with its
// current SHA-256; every SBOM passes CheckFile. The reports are sorted by
// file name.
func CheckDist(dir string, sum GoSum, specVersion string) ([]Report, error) {
	var meta metadata
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return nil, fmt.Errorf("goreleaser metadata.json: %w", err)
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("goreleaser metadata.json: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var sboms, archives, checksums []string
	for _, ent := range entries {
		name := ent.Name()
		switch {
		case ent.IsDir():
		case strings.HasSuffix(name, Suffix):
			sboms = append(sboms, name)
		case strings.HasSuffix(name, archiveSuffix):
			archives = append(archives, name)
		case strings.HasPrefix(name, "checksums") && strings.HasSuffix(name, ".txt"):
			checksums = append(checksums, name)
		}
	}
	sort.Strings(sboms)
	if len(sboms) == 0 {
		return nil, fmt.Errorf("%s: no SBOM (*%s)", dir, Suffix)
	}
	if len(checksums) != 1 {
		return nil, fmt.Errorf("%s: want one checksums*.txt, found %d", dir, len(checksums))
	}
	for _, a := range archives {
		want := strings.TrimSuffix(a, archiveSuffix) + Suffix
		if !contains(sboms, want) {
			return nil, fmt.Errorf("archive %s has no SBOM %s", a, want)
		}
	}
	for _, s := range sboms {
		want := strings.TrimSuffix(s, Suffix) + archiveSuffix
		if !contains(archives, want) {
			return nil, fmt.Errorf("SBOM %s has no archive %s", s, want)
		}
	}
	listed, err := readChecksums(filepath.Join(dir, checksums[0]))
	if err != nil {
		return nil, err
	}
	expect := Expect{Name: meta.ProjectName, Version: meta.Version, Tag: meta.Tag, SpecVersion: specVersion}
	var reports []Report
	for _, s := range sboms {
		path := filepath.Join(dir, s)
		want, ok := listed[s]
		if !ok {
			return nil, fmt.Errorf("SBOM %s is not listed in %s", s, checksums[0])
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		got := sha256.Sum256(data)
		if hex.EncodeToString(got[:]) != want {
			return nil, fmt.Errorf("SBOM %s: checksum in %s does not match the file", s, checksums[0])
		}
		rep, err := CheckFile(path, expect, sum)
		if err != nil {
			return nil, err
		}
		reports = append(reports, rep)
	}
	return reports, nil
}

// readChecksums parses a sha256sum-style file: "<hex>  <name>" per line.
func readChecksums(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	listed := map[string]string{}
	for _, line := range bytes.Split(data, []byte("\n")) {
		fields := strings.Fields(string(line))
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return nil, fmt.Errorf("%s: malformed line %q", filepath.Base(path), line)
		}
		listed[strings.TrimPrefix(fields[1], "*")] = fields[0]
	}
	return listed, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// GenerateOptions describes one cyclonedx-gomod run against a released binary.
type GenerateOptions struct {
	// Tool is the cyclonedx-gomod executable, found in PATH.
	Tool string
	// Binary is the built executable to catalog.
	Binary string
	// Output is the SBOM file to write.
	Output string
	// GOOS and GOARCH of the binary; they become the platform qualifiers of
	// the purls, which otherwise carry the host values.
	GOOS   string
	GOARCH string
	// Version of the release without the leading v.
	Version string
	// SpecVersion of CycloneDX to write.
	SpecVersion string
}

// Command returns the cyclonedx-gomod invocation for the options: name,
// arguments and the environment to add. GOPROXY=off keeps the license lookup
// to the local module cache so nothing is fetched during a release.
func Command(o GenerateOptions) (name string, args []string, env []string) {
	args = []string{
		"bin", "-json", "-licenses", "-std",
		"-output-version", o.SpecVersion,
		"-version", "v" + o.Version,
		"-output", o.Output,
		o.Binary,
	}
	env = []string{"GOOS=" + o.GOOS, "GOARCH=" + o.GOARCH, "GOPROXY=off"}
	return o.Tool, args, env
}

// ErrUsage marks a wrong invocation of the tool; main prints the usage then.
var ErrUsage = errors.New("usage")
