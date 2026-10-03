#!/usr/bin/env sh
# Runs the Go toolchain inside Docker so the host needs no Go install.
set -e
cd "$(dirname "$0")/.."
root="$(pwd -W 2>/dev/null || pwd)"
MSYS_NO_PATHCONV=1 exec docker run --rm -v "$root:/src" -v tollgate-gomod:/go/pkg/mod \
  -v tollgate-gobuild:/root/.cache/go-build -w /src golang:1.25 go "$@"
