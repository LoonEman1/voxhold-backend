#!/bin/sh
set -eu

# Produces exactly one API binary and one bootstrap binary for the target CPU.
cd "$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
out=${1:-dist}
mode=${GOAMD64:-v1}
arch=${GOARCH:-$(go env GOARCH)}
case "$mode" in v1|v3) ;; *) echo 'GOAMD64 must be v1 or v3' >&2; exit 2 ;; esac
if [ "$arch" != amd64 ] && [ "$mode" != v1 ]; then
    echo 'GOAMD64=v3 requires GOARCH=amd64' >&2
    exit 2
fi
export CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOAMD64="$mode"
mkdir -p "$out"
go build -trimpath -ldflags='-s -w' -pgo="${PGO:-auto}" -o "$out/voxhold" ./cmd/api
go build -trimpath -ldflags='-s -w' -pgo=off -o "$out/voxhold-bootstrap" ./cmd/bootstrap
