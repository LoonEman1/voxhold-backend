# Performance changes in 2.5.0

Four changes reduce repeated work: SQL notification candidates, shared encoding
for identical WebSocket broadcasts, isolated composer/memoized frontend rows,
and ordered history append with short listener batches. API JSON imports stay on
`encoding/json`; no migration to a different public JSON API is part of this release.

## Backend microbenchmarks

Measured on 30 October 2026 on a Ryzen 7 9800X3D, Windows amd64,
Go 1.27.3, GOMAXPROCS=1, five one-second samples per case. Medians:

| Case | Before | After | Allocated bytes before/after |
| --- | ---: | ---: | ---: |
| Notification delivery, 100 members, ordinary message | 434 µs | 202 µs | 33,703 / 5,403 |
| Notification delivery, 1,000 members, ordinary message | 2,587 µs | 292 µs | 306,904 / 5,397 |
| Notification delivery, 1,000 members, one mention | 2,595 µs | 360 µs | 307,279 / 6,469 |
| Broadcast encoding/queueing, 100 clients | 147 µs | 9.4 µs | 186,511 / 4,328 |
| Broadcast encoding/queueing, 1,000 clients | 1,468 µs | 76.7 µs | 1,877,116 / 38,928 |

The notification fixture uses a real SQLite WAL database, but a synthetic message
ID makes the final insert select no durable notification rows. It measures
candidate selection/transaction work, not end-to-end HTTP persistence. The
broadcast benchmark uses real hub queues and encoding without network I/O;
its speedup must not be described as the whole WebSocket transport speedup.

To repeat against a selected checkout:

```sh
GOMAXPROCS=1 go test ./internal/notifications ./internal/realtime \
  -run '^$' -bench 'Benchmark(NotificationDelivery|BroadcastEncoding)$' \
  -benchtime=1s -count=5 -benchmem
```

## HTTP and WebSocket protocol

The isolated local Docker comparison uses a synthetic database with 250 initial
messages; an additional case adds 1,000 members. Backend CPU 0 and generator CPU 2
are separate physical cores (CPU 0's SMT sibling is 1). Each container is capped
at one CPU and 512 MiB; backend GOMAXPROCS=1. Every sample starts from a fresh
copy of the same seeded database. Test-only rate limits are raised to 1,000,000.
No host ports are published and no production database or remote host is used.

Compare before-v1, after-v1 and after-v3 over three rotated rounds, two seconds
warmup and eight seconds measurement per sample. HTTP uses eight closed-loop
workers. WebSocket fanout uses 100 or 1,000 subscribed connections from the same
user: POST one message, wait until every connection receives its next ordered
`message.created`, then POST again. Fanout latency includes the HTTP request,
SQLite persistence, access checks, socket delivery and generator JSON decoding.
The one-core generator can limit the measured transport throughput.

Primary optimization comparison holds the instruction set at v1. The separate
v3 comparison measures compiler targeting and can be smaller than normal local
run-to-run variation. Preserve raw samples/ranges, and do not infer Selectel
performance or voice/video capacity from these text-message scenarios.

## Frontend

The matching frontend report documents the stressed Chromium development fixture:
[frontend performance](https://github.com/LoonEman1/voxhold-frontend/blob/main/docs/performance-2.5.0.md).
Typing 120 characters used median 8,830 -> 438 ms of JavaScript CPU time; the
1,000-message history burst took 704.8 -> 159.7 ms including its notification delay.
These are fixture measurements, not production FPS or keystroke latency.

## CPU images and size

See [AMD64 image selection](amd64-levels.md). Each image has one API variant;
v1 and v3 are separate tags. Portable tags retain amd64-v1/arm64. On the initial
same-source CPU comparison, API sizes were 18,825,376 bytes (v1) and 18,808,992
bytes (v3). A matching unstripped v1 build was 27,487,761 bytes: removing debug
sections reduced it by 31.5%, without a claimed runtime speedup. Sizes vary as
code changes; pulling one CPU tag does not pull both API binaries.
