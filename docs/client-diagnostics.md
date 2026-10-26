# Client diagnostics

Authenticated web clients send bounded diagnostic batches to
`POST /api/v1/diagnostics/client-events`. The events describe HTTP request
status and latency, WebSocket lifecycle and event types, WebRTC state, media
track lifecycle, playback failures and periodic RTP counters.

Updated clients also include interval media-quality summaries in RTP event
`quality`: RTT, direct/relay route, packet-loss percentage, concealed-audio
percentage, jitter-buffer delay, decode time and video freeze counters. Missing
interval measurements are null; `interval_available` is false on the first
sample. ICE candidate strings and addresses are still excluded.

Connection attempts have a `media_attempt_id`; connection-state events include
`connection_ms` and successful `recovery_ms`. `stream_first_frame` measures media
attachment to first presented frame, and `stream_playback_summary` records visible
playback stalls. `session_started.browser` identifies the browser family,
including Yandex. `main_thread_load` describes long tasks during visible-page
10-second intervals. These events use the same bounded, owner-only collector;
no database migration is required.

The collector does not intentionally store message bodies, passwords, access
tokens, cookies, SDP, ICE candidates or network addresses. Sensitive field
names are redacted independently by both the client and the backend. Diagnostic
payloads are still operational metadata and must be protected like the rest of
the Voxhold database.

Retention and size are bounded in the backend:

- events older than 24 hours are deleted during ingestion;
- one authenticated user can retain at most 10,000 events;
- the instance retains at most 50,000 events;
- one request is limited to 64 KiB and 64 events;
- one event's details are limited to 4 KiB.

Only the instance owner can read collected events:

```text
GET /api/v1/diagnostics/client-events?since=<unix-seconds>&limit=1000
GET /api/v1/diagnostics/client-events?session_id=<id>&category=media&limit=5000
```

The web client exposes the same owner-only export as **Server settings > Client
diagnostics > Download JSON**. A successful RTP connection does not prove that
a person perceived sound, but the combined network, track and playback fields
distinguish common failures such as missing inbound packets, muted or ended
tracks, packet loss and browser autoplay rejection.
