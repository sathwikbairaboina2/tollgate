# ADR 0008: The Go toolchain runs in Docker; the host needs no Go install

Date: 2026-10-03 · Status: accepted

## Decision

`scripts/go.ps1`, `scripts/go.sh` and the Makefile run `go` inside `golang:1.25`, with named volumes
for the module and build caches (`tollgate-gomod`, `tollgate-gobuild`). CI uses `actions/setup-go` natively.

## What I gave up

- **Edit-test latency.** Every run pays for container startup (roughly a second or two), and `-race`
  runs in a Linux container even on a Windows host.
- **IDE integration out of the box.** gopls on the host still needs a local Go install.
