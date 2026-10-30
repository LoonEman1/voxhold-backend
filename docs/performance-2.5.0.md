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

### HTTP/fanout results

Medians of three samples per variant, operations/second. The primary comparison
uses the same v1 instruction set before and after the application changes.

| Scenario | Before v1 | After v1 | Application change | After v3 |
| --- | ---: | ---: | ---: | ---: |
| Server list | 7051.32 | 7150.47 | +1.41% | 6953.25 |
| History (50 messages) | 1402.86 | 1418.83 | +1.14% | 1406.97 |
| Search | 627.05 | 639.63 | +2.01% | 643.78 |
| Write, small server | 1027.12 | 1069.48 | +4.12% | 1071.91 |
| Write, +1,000 members | 280.83 | 953.51 | +239.53% | 984.97 |
| Full fanout, 100 sockets | 320.21 | 354.78 | +10.80% | 353.08 |
| Full fanout, 1,000 sockets | 88.98 | 106.11 | +19.25% | 101.97 |
| News, 1,000 notification writes, 1 worker | 28.28 | 26.94 | -4.72% | 28.52 |

The main 63 samples completed 817,087 measured requests and delivered 9,628,300
WebSocket events with zero errors. Write/+1,000 members P95 fell from 33.76 to
13.54 ms; full 1,000-socket fanout P95 fell from 17.51 to 12.02 ms. Read scenarios
changed only around 1–2%, comparable to local variation. v3 had no consistent
universal advantage over the new v1 in these workloads.

The dense news case is a separate nine-sample run with one worker; no errors
occurred. It saves 1,000 actual notification rows per message and remains around
27–29 messages/second. Its ranges overlap (v1 before 27.87–28.52, after
25.66–28.50), so no dense-delivery improvement is claimed. An exploratory dense
run with eight workers hit one five-second client timeout after the change;
that saturation run is retained separately rather than included in the zero-error
main sample count. The unchanged timeout was not raised to conceal the limit.

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
