# AMD64 portability and optional v2 builds

Published images retain `GOAMD64=v1` as the minimum CPU requirement. This is
important for installations on third-party VMs: the guest-visible instruction
set depends on the hypervisor as well as the physical CPU. Go rejects an
unsupported minimum level at startup. The final image has no runtime GOAMD64
switch: this is a compilation setting.

For a controlled AMD64 environment advertising all v2 instructions:

```sh
docker build --build-arg GOAMD64=v2 -t voxhold-backend:local-v2 .
```

This compiles the API, bootstrap and migration tool consistently. ARM64 images
retain their own architecture settings. The public release remains portable.

## What was measured

Go 1.27.3, Linux AMD64, guest-visible Intel Xeon Gold 6240, GOMAXPROCS=1,
`-trimpath`, real SQLite WAL access-check benchmark with 100 users. Three short
alternating trials, 250ms benchmark target, low scheduling priority, disposable
fixture databases. These are preliminary workload-specific measurements, not an
end-to-end throughput claim or a statistically established whole-server gain.

| Trial | v1 ns/op | v2 ns/op |
| --- | ---: | ---: |
| 1 | 1,292,925 | 1,318,398 |
| 2 | 1,328,601 | 1,198,613 |
| 3 | 1,284,611 | 1,199,063 |

Median: 1.293ms vs 1.199ms. The variation and small sample size do not justify
raising the baseline for all installations. This project does not use the
Roaring Bitmap workload discussed in the linked article. Go can already choose
some optimized routines dynamically even with a v1 baseline.

Automatic v1/v2 selection is possible inside **one image** containing two
binaries and a v1-compatible CPU-detecting launcher. Separate images are not a
technical requirement. It increases image/build size and adds a startup path
that needs explicit fallback testing; it is deferred until broader profiling
shows enough benefit. Do not use a shell heuristic that treats one feature such
as POPCNT as proof of the entire v2 instruction set.

Reproduce in a Linux checkout with migrations available:

```sh
GOAMD64=v1 go test -run '^$' -bench '^BenchmarkLoadAccess$/^100$/^batch=true$' -benchtime=250ms -count=8 ./internal/channel/sqlite
GOAMD64=v2 go test -run '^$' -bench '^BenchmarkLoadAccess$/^100$/^batch=true$' -benchtime=250ms -count=8 ./internal/channel/sqlite
```

References: [Go minimum requirements](https://go.dev/wiki/MinimumRequirements#amd64),
[original microarchitecture benchmarks](https://lemire.me/blog/2026/06/06/how-much-do-amd64-microarchitecture-levels-help-in-go/),
[Avito overview](https://habr.com/ru/companies/avito/articles/1091694/).
