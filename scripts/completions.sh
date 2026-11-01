#!/bin/sh
# Generate shell completions for the release archives (run by goreleaser).
set -e
cd "$(dirname "$0")/.."
rm -rf completions
mkdir completions
go run ./cmd/total-recall completion bash > "completions/total-recall.bash"
go run ./cmd/total-recall completion zsh > "completions/_total-recall"
go run ./cmd/total-recall completion fish > "completions/total-recall.fish"
