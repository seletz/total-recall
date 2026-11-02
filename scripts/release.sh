#!/usr/bin/env bash
set -euo pipefail

# Interactive semver release: bump the latest v* tag, create an annotated tag
# and push it. The release workflow (.github/workflows/release.yml) does the rest.

if [ -n "$(git status --porcelain)" ]; then
    echo "Working tree is dirty; commit or stash first." >&2
    exit 1
fi

git fetch --tags --quiet origin

# Get the latest version tag, default to v0.0.0
LATEST=$(git tag -l 'v*' --sort=-v:refname | head -n1)
if [ -z "$LATEST" ]; then
    LATEST="v0.0.0"
fi

echo "Current version: $LATEST"
echo ""
echo "Select bump type:"
echo "  1) patch"
echo "  2) minor"
echo "  3) major"
echo ""
read -rp "Choice [1/2/3]: " CHOICE

# Strip leading 'v' for arithmetic
VERSION="${LATEST#v}"
IFS='.' read -r MAJOR MINOR PATCH <<< "$VERSION"

case "$CHOICE" in
    1|patch)
        PATCH=$((PATCH + 1))
        ;;
    2|minor)
        MINOR=$((MINOR + 1))
        PATCH=0
        ;;
    3|major)
        MAJOR=$((MAJOR + 1))
        MINOR=0
        PATCH=0
        ;;
    *)
        echo "Invalid choice"
        exit 1
        ;;
esac

NEXT="v${MAJOR}.${MINOR}.${PATCH}"

# The release workflow puts the section "## $NEXT" of docs/reference/90-aenderungen.md at
# the top of the GitHub release; without it the workflow fails after the tag
# is pushed, so check here first.
if ! "$(dirname "$0")/release-notes.sh" "$NEXT" >/dev/null; then
    echo "Add a section \"## $NEXT\" to docs/reference/90-aenderungen.md before tagging." >&2
    exit 1
fi

echo ""
read -rp "Create release $NEXT? [y/N]: " CONFIRM
if [[ "$CONFIRM" != "y" && "$CONFIRM" != "Y" ]]; then
    echo "Aborted."
    exit 0
fi

echo "Tagging $NEXT..."
git tag -a "$NEXT" -m "Release $NEXT"

echo "Pushing tag to origin..."
git push origin "$NEXT"

echo ""
echo "Done! Tag $NEXT pushed."
echo "GoReleaser will build artifacts, create the GitHub release, and update the Homebrew tap."
