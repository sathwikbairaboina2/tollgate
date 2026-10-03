# Runs the Go toolchain inside Docker so the host needs no Go install.
$root = (Resolve-Path "$PSScriptRoot\..").Path -replace '\\', '/'
docker run --rm -v "${root}:/src" -v tollgate-gomod:/go/pkg/mod -v tollgate-gobuild:/root/.cache/go-build -w /src golang:1.25 go @args
exit $LASTEXITCODE
