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

`stream_capture_ready` records the display surface and the browser's reported
audio capture route. The publisher's echo check records whether a test tone
was detected in the screen audio or the microphone. Diagnostic events contain
the results of these checks, without audio samples or device identifiers.

The collector does not intentionally store message bodies, passwords, access
tokens, cookies, SDP, ICE candidates or network addresses. Sensitive field
names are redacted independently by both the client and the backend. Diagnostic
payloads are still operational metadata and must be protected like the rest of
the Voxhold database.

Retention and size are bounded in the backend:

- the journal contains events received in the last 24 hours. A scheduled cleanup removes expired rows;
- the instance keeps up to 100 MB of diagnostic data, evicting the oldest events when it reaches the limit. The quota counts UTF-8 payload bytes plus row metadata in the shared SQLite database;
- one request is limited to 64 KiB and 64 events;
- one event's details are limited to 4 KiB.

Only the instance owner can read collected events:

```text
GET /api/v1/diagnostics/client-events?since=<unix-seconds>&limit=1000
GET /api/v1/diagnostics/client-events?session_id=<id>&category=media&limit=5000
GET /api/v1/diagnostics/client-events?limit=1000&before_created_at=<unix-seconds>&before_id=<id>
```

The web client follows `next_cursor` when downloading **Server settings > Client
diagnostics > Download JSON**, starting with the newest events and stopping at 100 MB. A successful RTP connection does not prove that
a person perceived sound, but the combined network, track and playback fields
distinguish common failures such as missing inbound packets, muted or ended
tracks, packet loss and browser autoplay rejection.
