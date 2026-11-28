#!/usr/bin/env bash
set -euo pipefail

# Build the static DuckDB libraries total-recall-analytics links: DuckDB at
# the commit of analytics/duckdb/duckdb.lock with the extensions of
# analytics/duckdb/extensions.cmake compiled in (sqlite_scanner among them),
# plus the generated static extension loader without the loadable_extensions
# capability. The binary then never loads an extension at run time: no
# dlopen, no download, no extension file next to it.
#
# Usage:
#   scripts/analytics-duckdb.sh                  # the platform of `go env`
#   scripts/analytics-duckdb.sh darwin_amd64     # GOOS_GOARCH; darwin_amd64
#                                                # also cross-builds on arm64
#
# Output: analytics/duckdb/lib/<goos>_<goarch>/*.a (gitignored), linked by
# analytics/internal/duck/cgo_<goos>_<goarch>.go. A stamp with the hash of
# lock, extension config and this script skips rebuilding when nothing
# changed. Needs git, cmake, make, python3 and a C++ compiler; network only
# for the DuckDB and extension sources.

ROOT=$(cd "$(dirname "$0")/.." && pwd)
DIR="$ROOT/analytics/duckdb"
WORK=${ANALYTICS_DUCKDB_WORK:-"$ROOT/build/duckdb"}
read -r _ VERSION COMMIT < <(grep '^duckdb ' "$DIR/duckdb.lock")
EXTENSIONS="core_functions;icu;json;parquet;sqlite_scanner"

sha() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum | cut -d' ' -f1; else shasum -a 256 | cut -d' ' -f1; fi
}
STAMP=$(cat "$DIR/duckdb.lock" "$DIR/extensions.cmake" "$0" | sha)

cpus() { getconf _NPROCESSORS_ONLN 2>/dev/null || sysctl -n hw.ncpu; }

build() {
  local platform=$1 os=${1%_*} arch=${1#*_} out src
  out="$DIR/lib/$platform"
  if [ -f "$out/stamp" ] && [ "$(cat "$out/stamp")" = "$STAMP" ]; then
    echo "$platform: DuckDB $VERSION present"
    return
  fi
  src="$WORK/$platform"
  if [ ! -d "$src/.git" ]; then
    git init -q "$src"
    git -C "$src" remote add origin https://github.com/duckdb/duckdb.git
  fi
  git -C "$src" fetch -q --depth 1 origin "$COMMIT"
  git -C "$src" checkout -q --force FETCH_HEAD
  local vars=()
  if [ "$os" = darwin ]; then
    case "$arch" in
      amd64) vars+=(OSX_BUILD_ARCH=x86_64) ;;
      arm64) vars+=(OSX_BUILD_ARCH=arm64) ;;
    esac
  fi
  echo "$platform: building DuckDB $VERSION with $EXTENSIONS"
  (
    cd "$src"
    env CMAKE_BUILD_PARALLEL_LEVEL="$(cpus)" DISABLE_SANITIZER=1 BUILD_SHELL=0 BUILD_UNITTESTS=0 \
      CORE_EXTENSIONS="" EXTENSION_CONFIGS="$DIR/extensions.cmake" OVERRIDE_GIT_DESCRIBE="$VERSION" \
      "${vars[@]}" make release
    DUCKDB_CAPABILITIES=none STATICALLY_LINK_EXTENSIONS="$EXTENSIONS" make static_extension_loader
  )
  rm -rf "$out"
  mkdir -p "$out"
  local cc=(cc)
  [ "$os" = darwin ] && [ "$arch" = amd64 ] && cc=(cc -arch x86_64)
  [ "$os" = darwin ] && [ "$arch" = arm64 ] && cc=(cc -arch arm64)
  "${cc[@]}" -O2 -c -I"$src/src/include" "$src/build/release/static_extension_loader.c" -o "$out/loader.o"
  ar rcs "$out/libduckdb_static_extension_loader.a" "$out/loader.o"
  rm "$out/loader.o"
  cp "$src/build/release/src/libduckdb_static.a" "$out/"
  for e in ${EXTENSIONS//;/ }; do
    cp "$src/build/release/extension/$e/lib${e}_extension.a" "$out/"
  done
  echo "$STAMP" >"$out/stamp"
  echo "$platform: DuckDB $VERSION built into $out"
}

if [ $# -eq 0 ]; then
  set -- "${GOOS:-$(go env GOOS)}_${GOARCH:-$(go env GOARCH)}"
fi
for p in "$@"; do
  build "$p"
done
