//go:build !(darwin && (arm64 || amd64)) && !(linux && (amd64 || arm64))

package duck

// Platform is empty: DuckDB publishes no embedded extension for this build.
const Platform = ""

var sqliteExtGz []byte
