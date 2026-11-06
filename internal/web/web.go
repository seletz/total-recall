// Package web serves the web UI of `total-recall serve`: the Svelte
// application in web/ (built by Vite into dist/ and embedded with go:embed)
// and the small JSON API it reads from, on the loopback interface only.
//
// The package knows nothing about the store's query types: the CLI hands it
// functions that answer the API routes with the same JSON structs as the
// --json output of search, stats and status, so the UI reads the contract
// the CLI already publishes. Everything about the archive stays in
// internal/cli and internal/store.
//
// dist/ is git-ignored except for .gitkeep: `go build` works without the
// built UI and serves a placeholder page that says so (see Built).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the embedded UI build rooted at dist/.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("web: dist directory missing from embed: " + err.Error())
	}
	return sub
}

// Built reports whether fsys holds a UI build (an index.html).
func Built(fsys fs.FS) bool {
	info, err := fs.Stat(fsys, "index.html")
	return err == nil && !info.IsDir()
}
