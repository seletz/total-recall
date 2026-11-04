#!/usr/bin/env bash
set -euo pipefail

# Check that the release tools are pinned to one version everywhere. mise.toml
# is the source of truth: `goreleaser = "X"` under [tools] and the
# cyclonedx-gomod pin of the task release:snapshot. Every goreleaser-action
# step in .github/workflows/ must pin `version: vX`, every `go install` of
# cyclonedx-gomod the same tag, and every workflow that runs goreleaser-action
# must install cosign first, otherwise the action skips the signature check of
# the goreleaser download. A mismatch is printed and the script exits 1.
# `mise run tools:check` and the CI call it.
#
# Usage: scripts/tool-versions-check.sh

ROOT=$(cd "$(dirname "$0")/.." && pwd)
MISE="$ROOT/mise.toml"
WORKFLOWS="$ROOT/.github/workflows"
status=0

fail() {
  echo "::error::$*" >&2
  status=1
}

goreleaser=$(sed -nE 's/^goreleaser = "([0-9][^"]*)"$/\1/p' "$MISE")
cyclonedx=$(sed -nE 's/.*"github:CycloneDX\/cyclonedx-gomod" = "([0-9][^"]*)".*/\1/p' "$MISE")
[ -n "$goreleaser" ] || { fail "mise.toml: no fixed goreleaser version under [tools] (goreleaser = \"X.Y.Z\")"; }
[ -n "$cyclonedx" ] || { fail "mise.toml: no cyclonedx-gomod pin in task release:snapshot"; }
echo "mise.toml: goreleaser ${goreleaser:-?}, cyclonedx-gomod ${cyclonedx:-?}"

for wf in "$WORKFLOWS"/*.yml; do
  name=$(basename "$wf")
  actions=$(grep -c 'uses: goreleaser/goreleaser-action@' "$wf" || true)
  if [ "$actions" -gt 0 ]; then
    # The `version:` of every goreleaser-action step (the input that follows
    # its `uses:` line) must be the pinned mise version; a step without one
    # would float to the latest release.
    pinned=$(awk -v want="v$goreleaser" '
      /^[[:space:]]*- / { if (inaction && !seen) bad++; inaction = 0; seen = 0 }
      /uses: goreleaser\/goreleaser-action@/ { inaction = 1; seen = 0; next }
      inaction && /^[[:space:]]+version:/ {
        v = $2; gsub(/"/, "", v); seen = 1
        if (v == want) ok++; else { bad++; sub(/^[[:space:]]+/, "", $0); print "  found " $0 }
      }
      END { if (inaction && !seen) bad++; print ok+0 " ok, " bad+0 " bad" }
    ' "$wf")
    summary=$(echo "$pinned" | tail -n 1)
    if [ "$summary" != "$actions ok, 0 bad" ]; then
      fail "$name: every goreleaser-action step must pin version: v$goreleaser ($summary)"
      echo "$pinned" | sed '$d' >&2
    fi
    if ! grep -q 'uses: sigstore/cosign-installer@' "$wf"; then
      fail "$name: runs goreleaser-action without sigstore/cosign-installer; the download signature would not be checked"
    fi
  fi
  while IFS= read -r line; do
    tag=${line##*cyclonedx-gomod@}
    if [ "$tag" != "v$cyclonedx" ]; then
      fail "$name: installs cyclonedx-gomod@$tag, mise.toml pins v$cyclonedx"
    fi
  done < <(grep -oE 'cyclonedx-gomod@v[^ ]+' "$wf" || true)
done

if [ "$status" -eq 0 ]; then
  echo "workflows pin goreleaser v$goreleaser and cyclonedx-gomod v$cyclonedx"
fi
exit "$status"
