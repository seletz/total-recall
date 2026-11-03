package sbom

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic module set of the fixtures: two libraries plus the standard
// library, nothing from a real go.sum.
const (
	libA        = "example.com/alpha"
	libAVersion = "v1.2.0"
	libB        = "example.com/beta/v2"
	libBVersion = "v2.0.0-20250101000000-0123456789ab"
	goVersion   = "go1.99.0"
)

var (
	libAHex = strings.Repeat("ab", 32)
	libBHex = strings.Repeat("cd", 32)
)

func h1(hexDigest string) string {
	raw, err := hex.DecodeString(hexDigest)
	if err != nil {
		panic(err)
	}
	return "h1:" + base64.StdEncoding.EncodeToString(raw)
}

// fixtureGoSum is a go.sum with the two libraries, their /go.mod lines and a
// module that is listed but not linked into the binary.
func fixtureGoSum() string {
	return strings.Join([]string{
		libA + " " + libAVersion + " " + h1(libAHex),
		libA + " " + libAVersion + "/go.mod " + h1(strings.Repeat("11", 32)),
		libB + " " + libBVersion + " " + h1(libBHex),
		libB + " " + libBVersion + "/go.mod " + h1(strings.Repeat("22", 32)),
		"example.com/unused v0.1.0 " + h1(strings.Repeat("33", 32)),
		"",
	}, "\n")
}

func mustGoSum(t *testing.T) GoSum {
	t.Helper()
	sum, err := ParseGoSum(strings.NewReader(fixtureGoSum()))
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func purl(module, version, goos, goarch string) string {
	return fmt.Sprintf("pkg:golang/%s@%s?goarch=%s&goos=%s&type=module", module, version, goarch, goos)
}

func fixtureComponent(module, version, hexDigest, goos, goarch string) map[string]any {
	c := map[string]any{
		"bom-ref": fmt.Sprintf("pkg:golang/%s@%s?type=module", module, version),
		"type":    "library",
		"name":    module,
		"version": version,
		"scope":   "required",
		"purl":    purl(module, version, goos, goarch),
	}
	if hexDigest != "" {
		c["hashes"] = []any{map[string]any{"alg": "SHA-256", "content": hexDigest}}
	}
	return c
}

// fixtureBOM mimics the output of `cyclonedx-gomod bin` after Finish: the
// main component is named after the project and carries the version without
// the leading v, its purl still has it.
func fixtureBOM(goos, goarch string) map[string]any {
	return map[string]any{
		"$schema":      "http://cyclonedx.org/schema/bom-1.6.schema.json",
		"bomFormat":    "CycloneDX",
		"specVersion":  "1.6",
		"serialNumber": "urn:uuid:00000000-0000-4000-8000-000000000000",
		"version":      1,
		"metadata": map[string]any{
			"timestamp": "2026-01-01T00:00:00Z",
			"component": map[string]any{
				"bom-ref": "pkg:golang/example.com/project@v1.0.0?type=module#cmd/project",
				"type":    "application",
				"name":    "project",
				"version": "1.0.0",
				"purl":    fmt.Sprintf("pkg:golang/example.com/project@v1.0.0?goarch=%s&goos=%s&type=module#cmd/project", goarch, goos),
				"externalReferences": []any{
					map[string]any{"url": "https://example.com/project", "type": "vcs"},
				},
			},
		},
		"components": []any{
			fixtureComponent(libA, libAVersion, libAHex, goos, goarch),
			fixtureComponent(libB, libBVersion, libBHex, goos, goarch),
			fixtureComponent("std", goVersion, "", goos, goarch),
		},
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func expect() Expect {
	return Expect{Name: "project", Version: "1.0.0", SpecVersion: "1.6"}
}

func TestParseGoSum(t *testing.T) {
	sum := mustGoSum(t)
	tests := []struct {
		name, module, version, wantHex string
		wantOK                         bool
	}{
		{"direct module", libA, libAVersion, libAHex, true},
		{"pseudo version", libB, libBVersion, libBHex, true},
		{"go.mod line is not a module hash", libA, libAVersion + "/go.mod", "", false},
		{"unknown version", libA, "v9.9.9", "", false},
		{"unknown module", "example.com/none", "v1.0.0", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := sum.Hash(tt.module, tt.version)
			if ok != tt.wantOK || got != tt.wantHex {
				t.Fatalf("Hash(%s, %s) = %q, %v; want %q, %v", tt.module, tt.version, got, ok, tt.wantHex, tt.wantOK)
			}
		})
	}
}

func TestParseGoSumRejectsMalformedLines(t *testing.T) {
	tests := []struct{ name, line string }{
		{"two fields", "example.com/x v1.0.0"},
		{"no h1 prefix", "example.com/x v1.0.0 sha256:abcd"},
		{"bad base64", "example.com/x v1.0.0 h1:***"},
		{"wrong digest length", "example.com/x v1.0.0 h1:" + base64.StdEncoding.EncodeToString([]byte("short"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseGoSum(strings.NewReader(tt.line + "\n")); err == nil {
				t.Fatalf("ParseGoSum(%q) accepted the line", tt.line)
			}
		})
	}
}

func TestParseAsset(t *testing.T) {
	tests := []struct {
		file    string
		want    Asset
		wantErr bool
	}{
		{"project-v1.0.0-darwin-arm64.cdx.json", Asset{Name: "project", Tag: "v1.0.0", OS: "darwin", Arch: "arm64", Ext: ".cdx.json"}, false},
		{"total-recall-v0.3.1-linux-amd64.tar.gz", Asset{Name: "total-recall", Tag: "v0.3.1", OS: "linux", Arch: "amd64", Ext: ".tar.gz"}, false},
		{"project-v1.0.0-rc.1-linux-amd64.cdx.json", Asset{Name: "project", Tag: "v1.0.0-rc.1", OS: "linux", Arch: "amd64", Ext: ".cdx.json"}, false},
		{"project-v1.0.0-darwin-arm64.json", Asset{}, true},
		{"project-1.0.0-darwin-arm64.cdx.json", Asset{}, true},
		{"project-v1.0.0-arm64.cdx.json", Asset{}, true},
		{"checksums-v1.0.0.txt", Asset{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			got, err := ParseAsset(tt.file)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseAsset(%q) error = %v, wantErr %v", tt.file, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParseAsset(%q) = %+v, want %+v", tt.file, got, tt.want)
			}
		})
	}
}

func TestFinish(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project-v1.0.0-linux-amd64.cdx.json")
	bom := fixtureBOM("linux", "amd64")
	main := bom["metadata"].(map[string]any)["component"].(map[string]any)
	main["name"] = "example.com/project"
	main["version"] = "v1.0.0"
	writeJSON(t, path, bom)

	if err := Finish(path, "project", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Finish wrote invalid JSON: %v", err)
	}
	gotMain := got["metadata"].(map[string]any)["component"].(map[string]any)
	if gotMain["name"] != "project" || gotMain["version"] != "1.0.0" {
		t.Errorf("main component = %s %s, want project 1.0.0", gotMain["name"], gotMain["version"])
	}
	if gotMain["purl"] != main["purl"] {
		t.Errorf("purl changed to %v", gotMain["purl"])
	}
	if got["serialNumber"] != bom["serialNumber"] || len(got["components"].([]any)) != 3 {
		t.Errorf("Finish altered more than the main component")
	}
	if got["version"] != float64(1) {
		t.Errorf("document version = %v, want 1", got["version"])
	}
	// The finished file passes the check as it is.
	if _, err := CheckFile(path, expect(), mustGoSum(t)); err != nil {
		t.Errorf("CheckFile after Finish: %v", err)
	}
}

func TestFinishLicenses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project-v1.0.0-linux-amd64.cdx.json")
	bom := fixtureBOM("linux", "amd64")
	comps := bom["components"].([]any)
	comps[0].(map[string]any)["evidence"] = map[string]any{"licenses": []any{
		map[string]any{"license": map[string]any{"id": "deprecated_GPL-2.0+"}},
		map[string]any{"license": map[string]any{"id": "MIT"}},
	}}
	comps[1].(map[string]any)["evidence"] = map[string]any{"licenses": []any{
		map[string]any{"license": map[string]any{"name": "custom"}},
	}}
	writeJSON(t, path, bom)

	if err := Finish(path, "project", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Components []struct {
			Evidence struct {
				Licenses []struct {
					License map[string]string `json:"license"`
				} `json:"licenses"`
			} `json:"evidence"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := [][]map[string]string{
		{{"name": "GPL-2.0+"}, {"id": "MIT"}},
		{{"name": "custom"}},
		nil,
	}
	for i, w := range want {
		var g []map[string]string
		for _, l := range got.Components[i].Evidence.Licenses {
			g = append(g, l.License)
		}
		if fmt.Sprint(g) != fmt.Sprint(w) {
			t.Errorf("component %d licenses = %v, want %v", i, g, w)
		}
	}
	if _, err := CheckFile(path, expect(), mustGoSum(t)); err != nil {
		t.Errorf("CheckFile after Finish: %v", err)
	}
}

func TestFinishErrors(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		body string
	}{
		{"missing file", ""},
		{"invalid json", "{"},
		{"no metadata", `{"bomFormat":"CycloneDX"}`},
		{"no component", `{"metadata":{"timestamp":"x"}}`},
		{"metadata null", `{"metadata":null}`},
		{"metadata wrong type", `{"metadata":[]}`},
		{"component wrong type", `{"metadata":{"component":"x"}}`},
		{"not an object", `[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "-")+".cdx.json")
			if tt.body != "" {
				if err := os.WriteFile(path, []byte(tt.body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := Finish(path, "project", "1.0.0"); err == nil {
				t.Fatal("Finish accepted the document")
			}
		})
	}
}

func TestCheckFileGood(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project-v1.0.0-darwin-arm64.cdx.json")
	writeJSON(t, path, fixtureBOM("darwin", "arm64"))

	rep, err := CheckFile(path, expect(), mustGoSum(t))
	if err != nil {
		t.Fatal(err)
	}
	want := Report{File: "project-v1.0.0-darwin-arm64.cdx.json", OS: "darwin", Arch: "arm64", Modules: 2, Go: goVersion}
	if rep != want {
		t.Errorf("Report = %+v, want %+v", rep, want)
	}
}

func TestCheckFileSnapshotVersion(t *testing.T) {
	// A snapshot names the file after the last tag but carries a snapshot
	// version; the file check only ties the version to the main component.
	dir := t.TempDir()
	path := filepath.Join(dir, "project-v0.9.0-linux-amd64.cdx.json")
	bom := fixtureBOM("linux", "amd64")
	main := bom["metadata"].(map[string]any)["component"].(map[string]any)
	main["version"] = "0.9.1-SNAPSHOT-abcdef0"
	main["purl"] = "pkg:golang/example.com/project@v0.9.1-SNAPSHOT-abcdef0?goarch=amd64&goos=linux&type=module#cmd/project"
	writeJSON(t, path, bom)

	e := expect()
	e.Version = "0.9.1-SNAPSHOT-abcdef0"
	if _, err := CheckFile(path, e, mustGoSum(t)); err != nil {
		t.Fatal(err)
	}
	e.Tag = "v1.0.0"
	if _, err := CheckFile(path, e, mustGoSum(t)); err == nil {
		t.Fatal("CheckFile accepted a file named after another tag")
	}
}

func TestCheckFileBad(t *testing.T) {
	setMain := func(key string, value any) func(map[string]any) {
		return func(bom map[string]any) {
			bom["metadata"].(map[string]any)["component"].(map[string]any)[key] = value
		}
	}
	setComponent := func(i int, key string, value any) func(map[string]any) {
		return func(bom map[string]any) {
			bom["components"].([]any)[i].(map[string]any)[key] = value
		}
	}
	tests := []struct {
		name    string
		file    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{"file name schema", "project-1.0.0-darwin-arm64.cdx.json", nil, "file name"},
		{"file name other project", "other-v1.0.0-darwin-arm64.cdx.json", nil, "file name"},
		{"bom format", "", func(b map[string]any) { b["bomFormat"] = "SPDX" }, "bomFormat"},
		{"spec version", "", func(b map[string]any) { b["specVersion"] = "1.5" }, "specVersion"},
		{"main name", "", setMain("name", "example.com/project"), "main component"},
		{"main version with v", "", setMain("version", "v1.0.0"), "main component"},
		{"main purl version", "", setMain("purl", "pkg:golang/example.com/project@v1.0.1?goarch=arm64&goos=darwin&type=module#cmd/project"), "purl"},
		{"main purl platform", "", setMain("purl", "pkg:golang/example.com/project@v1.0.0?goarch=amd64&goos=linux&type=module#cmd/project"), "purl"},
		{"main purl without platform", "", setMain("purl", "pkg:golang/example.com/project@v1.0.0?type=module#cmd/project"), "purl"},
		{"main vcs reference other repo", "", setMain("externalReferences", []any{map[string]any{"url": "https://example.com/other", "type": "vcs"}}), "vcs"},
		{"main vcs reference missing", "", setMain("externalReferences", []any{}), "vcs"},
		{"no main component", "", func(b map[string]any) { delete(b["metadata"].(map[string]any), "component") }, "main component"},
		{"metadata null", "", func(b map[string]any) { b["metadata"] = nil }, "main component"},
		{"metadata wrong type", "", func(b map[string]any) { b["metadata"] = "x" }, "JSON"},
		{"components null", "", func(b map[string]any) { b["components"] = nil }, "no components"},
		{"components wrong type", "", func(b map[string]any) { b["components"] = map[string]any{} }, "JSON"},
		{"hashes wrong type", "", setComponent(0, "hashes", "abc"), "JSON"},
		{"no components", "", func(b map[string]any) { b["components"] = []any{} }, "no components"},
		{"module not in go.sum", "", func(b map[string]any) {
			setComponent(0, "version", "v1.3.0")(b)
			setComponent(0, "purl", purl(libA, "v1.3.0", "darwin", "arm64"))(b)
		}, "go.sum"},
		{"module version differs from purl", "", setComponent(0, "version", "v1.3.0"), "purl"},
		{"module hash differs", "", setComponent(0, "hashes", []any{map[string]any{"alg": "SHA-256", "content": strings.Repeat("ff", 32)}}), "hash"},
		{"module hash other algorithm", "", setComponent(0, "hashes", []any{map[string]any{"alg": "SHA-1", "content": strings.Repeat("ff", 20)}}), "hash"},
		{"module without hash", "", func(b map[string]any) { delete(b["components"].([]any)[1].(map[string]any), "hashes") }, "hash"},
		{"module purl platform", "", setComponent(1, "purl", purl(libB, libBVersion, "linux", "amd64")), "purl"},
		{"std missing", "", func(b map[string]any) { b["components"] = b["components"].([]any)[:2] }, "std"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			file := tt.file
			if file == "" {
				file = "project-v1.0.0-darwin-arm64.cdx.json"
			}
			bom := fixtureBOM("darwin", "arm64")
			if tt.mutate != nil {
				tt.mutate(bom)
			}
			path := filepath.Join(dir, file)
			writeJSON(t, path, bom)
			_, err := CheckFile(path, expect(), mustGoSum(t))
			if err == nil {
				t.Fatal("CheckFile accepted the document")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not mention %q", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), file) {
				t.Errorf("error %q does not name the file", err)
			}
		})
	}
}

func TestCheckFileNotJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project-v1.0.0-darwin-arm64.cdx.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckFile(path, expect(), mustGoSum(t)); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("CheckFile error = %v, want a JSON error", err)
	}
	if _, err := CheckFile(filepath.Join(dir, "project-v1.0.0-linux-arm64.cdx.json"), expect(), mustGoSum(t)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("CheckFile on a missing file: %v", err)
	}
}

// dist writes a goreleaser-like dist directory: metadata.json, one archive
// and one SBOM per platform and a checksum file that lists both.
type dist struct {
	t   *testing.T
	dir string
}

func newDist(t *testing.T, platforms ...string) dist {
	t.Helper()
	d := dist{t: t, dir: t.TempDir()}
	d.write("metadata.json", []byte(`{"project_name":"project","tag":"v1.0.0","version":"1.0.0"}`))
	var sums []string
	for _, p := range platforms {
		goos, goarch, _ := strings.Cut(p, "/")
		base := "project-v1.0.0-" + goos + "-" + goarch
		archive := []byte("archive " + p)
		d.write(base+".tar.gz", archive)
		sums = append(sums, sha256Line(archive, base+".tar.gz"))
		bom, err := json.MarshalIndent(fixtureBOM(goos, goarch), "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		d.write(base+".cdx.json", bom)
		sums = append(sums, sha256Line(bom, base+".cdx.json"))
	}
	d.write("checksums-v1.0.0.txt", []byte(strings.Join(sums, "\n")+"\n"))
	return d
}

func sha256Line(data []byte, name string) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]) + "  " + name
}

func (d dist) write(name string, data []byte) {
	d.t.Helper()
	if err := os.WriteFile(filepath.Join(d.dir, name), data, 0o644); err != nil {
		d.t.Fatal(err)
	}
}

func (d dist) remove(name string) {
	d.t.Helper()
	if err := os.Remove(filepath.Join(d.dir, name)); err != nil {
		d.t.Fatal(err)
	}
}

func (d dist) edit(name string, f func(string) string) {
	d.t.Helper()
	data, err := os.ReadFile(filepath.Join(d.dir, name))
	if err != nil {
		d.t.Fatal(err)
	}
	d.write(name, []byte(f(string(data))))
}

func TestCheckDistGood(t *testing.T) {
	d := newDist(t, "darwin/arm64", "linux/amd64")
	reps, err := CheckDist(d.dir, mustGoSum(t), "1.6")
	if err != nil {
		t.Fatal(err)
	}
	if len(reps) != 2 {
		t.Fatalf("got %d reports, want 2", len(reps))
	}
	if reps[0].File != "project-v1.0.0-darwin-arm64.cdx.json" || reps[1].File != "project-v1.0.0-linux-amd64.cdx.json" {
		t.Errorf("reports in unexpected order: %+v", reps)
	}
}

func TestCheckDistBad(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(d dist)
		wantErr string
	}{
		{"no sboms", func(d dist) {
			d.remove("project-v1.0.0-darwin-arm64.cdx.json")
			d.edit("checksums-v1.0.0.txt", func(s string) string { return strings.ReplaceAll(s, "project-v1.0.0-darwin-arm64.cdx.json", "x") })
		}, "no SBOM"},
		{"archive without sbom", func(d dist) {
			d.write("project-v1.0.0-linux-arm64.tar.gz", []byte("x"))
		}, "project-v1.0.0-linux-arm64.tar.gz"},
		{"sbom without archive", func(d dist) {
			d.remove("project-v1.0.0-darwin-arm64.tar.gz")
		}, "project-v1.0.0-darwin-arm64.tar.gz"},
		{"sbom not in checksums", func(d dist) {
			d.edit("checksums-v1.0.0.txt", func(s string) string {
				var kept []string
				for _, l := range strings.Split(s, "\n") {
					if !strings.HasSuffix(l, "darwin-arm64.cdx.json") {
						kept = append(kept, l)
					}
				}
				return strings.Join(kept, "\n")
			})
		}, "checksums-v1.0.0.txt"},
		{"sbom checksum stale", func(d dist) {
			d.edit("project-v1.0.0-darwin-arm64.cdx.json", func(s string) string { return s + "\n" })
		}, "checksum"},
		{"no checksum file", func(d dist) { d.remove("checksums-v1.0.0.txt") }, "checksums"},
		{"two checksum files", func(d dist) { d.write("checksums-v1.0.1.txt", []byte("")) }, "checksums"},
		{"no metadata", func(d dist) { d.remove("metadata.json") }, "metadata.json"},
		{"sbom of another tag", func(d dist) {
			d.edit("metadata.json", func(string) string { return `{"project_name":"project","tag":"v1.0.1","version":"1.0.1"}` })
		}, "v1.0.1"},
		{"invalid sbom", func(d dist) {
			d.edit("project-v1.0.0-linux-amd64.cdx.json", func(s string) string {
				return strings.Replace(s, `"specVersion": "1.6"`, `"specVersion": "1.4"`, 1)
			})
			d.edit("checksums-v1.0.0.txt", func(s string) string {
				data, err := os.ReadFile(filepath.Join(d.dir, "project-v1.0.0-linux-amd64.cdx.json"))
				if err != nil {
					t.Fatal(err)
				}
				var kept []string
				for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
					if strings.HasSuffix(l, "linux-amd64.cdx.json") {
						l = sha256Line(data, "project-v1.0.0-linux-amd64.cdx.json")
					}
					kept = append(kept, l)
				}
				return strings.Join(kept, "\n") + "\n"
			})
		}, "specVersion"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDist(t, "darwin/arm64", "linux/amd64")
			tt.mutate(d)
			_, err := CheckDist(d.dir, mustGoSum(t), "1.6")
			if err == nil {
				t.Fatal("CheckDist accepted the directory")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestCommand(t *testing.T) {
	opts := GenerateOptions{Tool: "cyclonedx-gomod", Binary: "dist/x/project", Output: "dist/project-v1.0.0-linux-arm64.cdx.json", GOOS: "linux", GOARCH: "arm64", Version: "1.0.0", SpecVersion: "1.6"}
	name, args, env := Command(opts)
	if name != "cyclonedx-gomod" {
		t.Errorf("command = %q", name)
	}
	wantArgs := []string{"bin", "-json", "-licenses", "-std", "-output-version", "1.6", "-version", "v1.0.0", "-output", opts.Output, opts.Binary}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Errorf("args = %q, want %q", args, wantArgs)
	}
	for _, want := range []string{"GOOS=linux", "GOARCH=arm64", "GOPROXY=off"} {
		found := false
		for _, e := range env {
			found = found || e == want
		}
		if !found {
			t.Errorf("env %q missing in %q", want, env)
		}
	}
}
