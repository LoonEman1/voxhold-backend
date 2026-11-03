# SQLite tuning measurements for 2.6.4

The draft proposed hourly `PRAGMA optimize` and `temp_store=MEMORY`.
The release retains the existing production settings. The measurements below
do not justify enabling both changes by default, and no hard 768 MiB Linux
container RSS measurement was available on the development host.

Reproduce with:

```sh
go test ./internal/message/sqlite -run '^$' -bench '^BenchmarkMessageSQLite$' -benchtime=500ms -count=3 -benchmem
```

The benchmark uses the production migrations, a file-backed WAL database,
100,000 messages across 20 channels, full-text search, real access checks and
interaction hydration. Each query returns 50 messages. Setup is outside the
timed loop. The optimized scenario runs `PRAGMA optimize=0x10002` after seeding;
the memory scenario also sets `temp_store=MEMORY` on the single test connection.

Measured on 2026-11-03 with Go 1.27.3, modernc SQLite 1.61.1, Windows/amd64,
AMD Ryzen 7 9800X3D. Medians of three runs, milliseconds per operation:

| Configuration | Search | Channel history |
| --- | ---: | ---: |
| Existing defaults | 4.790 | 0.441 |
| Optimize | 4.594 | 0.533 |
| Memory + optimize | 4.600 | 0.531 |

Optimize improves this search sample by about 4%, while history is about 21%
slower. The absolute difference is small, but the result is not a general
performance improvement. Memory temp storage adds no clear benefit over
optimize here. These synthetic, single-host results are not production latency
or a container memory guarantee. Allocations are about 111 KiB/search and
57 KiB/history in all scenarios.

The PR therefore adds reproducible baselines and records the decision instead
of changing runtime defaults. Reconsider maintenance and temp storage after a
representative Linux workload and peak RSS measurement under the real limit.
