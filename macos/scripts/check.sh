#!/usr/bin/env bash
# Check the build host of the macOS spike: macOS with the SDK (SwiftUI,
# AppKit, PDFKit) and the Swift version mise.toml pins for the macos tasks.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
if [ "$(uname -s)" != "Darwin" ]; then
  echo "macos: the app builds on macOS only (SwiftUI, AppKit)" >&2
  exit 1
fi
if ! xcrun --sdk macosx --show-sdk-path >/dev/null 2>&1; then
  echo "macos: the macOS SDK is missing; install Xcode and run xcode-select" >&2
  exit 1
fi
want=$(sed -nE 's/.*tools = \{ "swift" = "([0-9]+\.[0-9]+\.[0-9]+)" \}.*/\1/p' "$ROOT/mise.toml" | sort -u)
have=$(swift --version 2>/dev/null | sed -nE 's/.*Swift version ([0-9]+\.[0-9]+(\.[0-9]+)?).*/\1/p' | head -n 1)
if [ -z "$want" ] || [ "$have" != "$want" ]; then
  echo "macos: Swift ${have:-?} found, mise.toml pins ${want:-?}; run the macos tasks with mise run" >&2
  exit 1
fi
