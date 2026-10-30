# AMD64 v1/v3 images and build optimization

Each release publishes two explicit AMD64 tags in
`ghcr.io/looneman1/voxhold-backend`:

- `X.Y.Z-amd64-v1`: portable AMD64 baseline.
- `X.Y.Z-amd64-v3`: requires the full x86-64-v3 instruction set and OS AVX support.

The existing `X.Y.Z`, `X.Y` and `latest` tags remain multi-platform (AMD64 v1
and ARM64). Matching minor/latest aliases exist for both AMD64 variants. Each
image contains exactly one API executable and one bootstrap executable; there
is no second CPU copy or v2 variant. The migration tool stays on the v1 baseline.

`voxhold-deploy` selects the appropriate official image in both `install.sh`
and `update.sh`. The glibc loader verifies the complete x86-64-v3 level, including
OS support. If that detector is unavailable (for example, on a musl host), auto
selection conservatively uses v1. Older releases without suffixed tags use the
regular portable tag. Custom repositories and digest pins are preserved.

After pulling, deployment runs `voxhold --check-cpu` before starting services
or stopping the current backend for a backup. Go's runtime checks its compiled
instruction-set requirement before main; this command then exits without
opening the database. Rollback retains the exact saved image/digest.

Set `VOXHOLD_AMD64="v1"` in the deploy `.env` to force the baseline. `auto` is
the default; forcing `v3` requires a positive CPU/OS check and a published tag.
These are deploy settings; `GOAMD64` is a compile-time setting.

## Local and SSH builds

```sh
# Run in a Linux checkout, including one accessed over SSH on Selectel.
GOAMD64=v1 sh scripts/build-linux.sh dist/v1
GOAMD64=v3 sh scripts/build-linux.sh dist/v3

# One CPU variant per image. ARM64 builds use the default v1 argument (ignored
# by Go on ARM64); the v3 argument is accepted only for an AMD64 target.
docker build --build-arg GOAMD64=v1 -t voxhold-backend:local-amd64-v1 .
docker build --build-arg GOAMD64=v3 -t voxhold-backend:local-amd64-v3 .
docker run --rm --network none --read-only voxhold-backend:local-amd64-v3 voxhold --check-cpu
```

Builds use `CGO_ENABLED=0`, `-trimpath` and `-ldflags='-s -w'`. The linker flags
remove debug sections to reduce file/image size; they do not increase execution
speed. Function names remain available to Go pprof, but DWARF-based debugging
needs an unstripped development build. Compiler optimizations stay enabled.

PGO is available via `PGO=/path/to/cpu.pprof sh scripts/build-linux.sh` or Docker
`--build-arg PGO=profiles/cpu.pprof` (the profile must be in the build context).
The default `PGO=auto` also picks up `cmd/api/default.pgo` when one exists. Use a
representative production CPU profile from the existing loopback diagnostics
endpoint, not a small synthetic benchmark. Do not expose pprof publicly.
No profile is supplied by this change, so it does not claim a PGO speedup.

## JSON

Go 1.27.3 already implements `encoding/json` through the v2 engine. Voxhold keeps
the current public API and JSON semantics. Switching imports to
`encoding/json/v2` alone is not a justified whole-application performance claim,
and its different defaults would require a separate compatibility migration.
No extra `GOEXPERIMENT=jsonv2` flag is needed on this Go version.

## Measurements

The previous v1/v2 SQLite microbenchmarks were preliminary, workload-specific
measurements. They do not establish a v3 or whole-server speedup. Compare v1/v3
with representative HTTP/WebSocket/SQLite/media workloads before making such a
claim. AVX2 support alone is insufficient proof of v3 compatibility.

References: [Go 1.27 JSON changes](https://go.dev/doc/go1.27#encodingjsonv2),
[Go JSON migration guide](https://go.dev/doc/jsonv2-migration),
[Go minimum requirements](https://go.dev/wiki/MinimumRequirements#amd64),
[Go PGO](https://go.dev/doc/pgo).
