#!/usr/bin/env bash
set -euo pipefail

# Print the release notes of one version from docs/reference/90-aenderungen.md: the
# section whose heading is exactly "## <tag>" up to the next "## " heading,
# with the heading itself dropped. A heading with a suffix, such as
# "## v1.3.0 (unveröffentlicht)", is not a release yet and fails like a
# missing section. Used by the release workflow as the header of the GitHub
# release body (goreleaser --release-header); the commit changelog that
# goreleaser generates follows below it. scripts/release_notes_test.go runs
# it against scripts/testdata/changelog.md (`mise run release:notes:check`).
#
# Usage: scripts/release-notes.sh v0.2.0 [docs/reference/90-aenderungen.md]

TAG="${1:?usage: $0 TAG [FILE]}"
FILE="${2:-$(dirname "$0")/../docs/reference/90-aenderungen.md}"

NOTES=$(awk -v tag="$TAG" '
    /^## / { in_section = ($0 == "## " tag) ; if (in_section) next }
    in_section { print }
' "$FILE")

if [ -z "${NOTES//[[:space:]]/}" ]; then
    # A heading that starts with the tag but goes on ("## v1.3.0 (unveröffentlicht)")
    # is the usual reason: the section exists but is not marked released.
    HEADING=$(grep -m1 -F "## $TAG " "$FILE" || true)
    if [ -n "$HEADING" ]; then
        echo "section \"$HEADING\" in $FILE is not released yet; make the heading \"## $TAG\"" >&2
    else
        echo "no section \"## $TAG\" in $FILE" >&2
    fi
    exit 1
fi

# Release notes are plain Markdown: turn the ++key++ syntax of the docs into
# code spans and relative doc links into links to the published site. Links
# are relative to docs/reference/ (where the changelog lives): a sibling page
# ("10-befehle.md") lands under reference/, "../explanation/x.md" at the root,
# and a bare anchor ("#behoben") on the changelog page itself.
printf '%s\n' "$NOTES" | perl -pe '
    s/\+\+(.+?)\+\+/`$1`/g;
    s{\]\((\.\./)?([a-z0-9/-]+)\.md(#[a-z0-9-]+)?\)}{"](https://seletz.github.io/total-recall/" . ($1 ? "" : "reference/") . "$2/" . ($3 // "") . ")"}ge;
    s{\]\((#[a-z0-9-]+)\)}{](https://seletz.github.io/total-recall/reference/90-aenderungen/$1)}g;
'
