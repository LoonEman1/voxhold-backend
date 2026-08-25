# Screen streaming

Screen streaming is available only to WebSocket clients that are already in
the same voice channel. Leaving voice, losing server access, deleting the
channel, or closing the WebSocket automatically stops publishing or watching.

The microphone keeps using the voice PeerConnection on UDP `50000`. Screen
video and optional system audio use a separate stream PeerConnection on UDP
`50001`, so stream congestion cannot block the voice media socket.

## Modes

- `server`: the publisher uploads one selected video track and optionally one
  Opus system-audio track to Voxhold. The server forwards them to at most 32
  viewers by default.
- `p2p`: the backend validates and relays offer/answer/ICE events, while media
  travels directly from the publisher to each viewer. The default limit is 8
  viewers. P2P exposes participant IP addresses and multiplies publisher upload
  bandwidth by the viewer count.

WebRTC encrypts each hop with DTLS-SRTP. In server mode encryption terminates at
the SFU and a new encrypted hop is created for every viewer. It is not end-to-end
encryption against the server.

## Experimental HDR

HDR is opt-in and available only in `server` mode. The browser first verifies
the captured frames, 10-bit codec path, WebGPU processing path, and the viewer's
HDR output. A publisher that passes those checks uploads two video renditions:

- a 10-bit BT.2020 PQ/HLG master for a verified HDR viewer;
- an 8-bit BT.709 rendition tone-mapped on the publisher GPU for SDR and
  unknown/legacy viewers.

The SFU does not transcode. It selects one declared rendition for each viewer,
forwards the matching encoded RTP, and rebuilds the negotiated color-space RTP
extension. If either the HDR probe or the SDR tone-map pipeline fails, the HDR
publication fails closed instead of sending washed-out HDR to SDR displays.
P2P remains SDR-only. `Auto` also remains SDR until the hardware/browser matrix
described below passes the product gate.

## Limits

Client quality settings configure the browser encoder. They are not trusted by
the server. For server mode Voxhold measures incoming RTP payload and closes a
publisher that exceeds the configured limit:

```env
WEBRTC_STREAM_UDP_PORT=50001
WEBRTC_STREAM_MAX_VIEWERS=32
WEBRTC_STREAM_MAX_P2P_VIEWERS=8
WEBRTC_STREAM_MAX_VIDEO_BITRATE_KBPS=16000
WEBRTC_STREAM_MAX_AUDIO_BITRATE_KBPS=320
```

The client can select VP8, VP9, H.264, or AV1 when both its WebRTC sender and
receiver support the codec. Automatic selection follows the RFC 7742
compatibility policy: H.264 first, then VP8, VP9, and AV1. For H.264 the SFU
offers Constrained Baseline (`profile-level-id=42e01f`) first and keeps the
legacy `42001f` value on a separate payload type for older web clients. Every
primary video payload type is paired with a `video/rtx` retransmission payload
type (`apt=<primary PT>`), so lost packets can be repaired with NACK/RTX before
they turn into a freeze. The SFU forwards the selected encoded track without
transcoding and never substitutes one H.264 profile for another after ingest,
so every viewer must support that stream's codec.

The browser marks captured video as detailed screen content and uses
`maintain-resolution` degradation. During a short bandwidth shortage it should
prefer reducing frame rate over blurring text. The UI samples WebRTC stats every
two seconds and shows the actual resolution, frame rate, bitrate, codec, packet
loss, and whether bandwidth or CPU is limiting the encoder.

Pion's default NACK and transport-wide congestion-control interceptors are
enabled. The SFU asks for a key frame when video starts, when a viewer joins,
or when a viewer sends PLI/FIR; it does not force periodic key frames.

Temporary media-path failures do not immediately remove a publisher or viewer.
Server-mode streams and voice sessions wait briefly for ICE to recover, then
renegotiate fresh ICE credentials with bounded retries and a final grace period.
P2P viewers request the publisher to perform the restart; the backend only
relays this request for an existing publisher-viewer pair. Trickle candidates
from an older ICE generation are discarded on both sides instead of terminating
the current session.

Only one stream can be active in a voice channel. A publisher may send one
or, for experimental HDR, two declared video tracks in the same codec family,
plus at most one common Opus audio track. The video bitrate ceiling applies to
the sum of both renditions. Pending ICE candidates are capped at 64 per
server-side media session. The common WebSocket event limit also caps SDP and
P2P signaling payloads.

## Single-node boundary

Voxhold is a single-node SFU. SQLite data, WebSocket presence, rooms, rate
limits, ICE sessions, and the UDP mux are owned by one backend process. Starting
a second backend replica behind round-robin HTTP does not provide HA: signaling
can land on a process that does not own the media session, and in-memory limits
and room state diverge. Use one backend replica with durable off-host backups.

Real multi-node HA is a separate architecture: shared durable application and
signaling state, distributed rate limits/presence, sticky routing, and explicit
media-room ownership with failover or session migration. SQLite file sharing
and two processes binding different UDP ports are not substitutes for it.

The default 32-viewer limit is a safety ceiling, not a capacity guarantee.
Dual-rendition HDR approximately doubles publisher encode work and upload; SFU
egress still grows with the selected rendition bitrate times viewer count.

## Runtime ICE configuration for browsers

The backend is the single runtime source of ICE configuration. An authorized
client fetches it with:

```
GET /api/v1/webrtc/config
Authorization: Bearer <token>
Cache-Control: no-store
```

The response mirrors the validated `WEBRTC_ICE_*` environment values (empty
`ice_servers` is valid) and always reports `ice_transport_policy: "all"`, so
direct UDP stays the primary path and TURN is only an ICE fallback. The
frontend keeps this configuration in memory only and never embeds TURN values
at build time.

## Viewer-initiated recovery

A server-mode viewer can send `stream.recovery_request` with
`{ "action": "keyframe" | "ice_restart" }`. The hub allows the request only
from a current viewer of that server-mode stream; publishers, P2P participants,
and foreign channels receive an error. The media session enforces per-viewer
cooldowns: keyframe requests are accepted at most once every 2 seconds and ICE
restarts at most once every 10 seconds; a rate-limited request is silently
ignored and never closes the session. There are no unconditional periodic PLI
requests.

On a VPS, allow both UDP ports in the host and provider firewall and set
`WEBRTC_PUBLIC_IP` to the public IPv4 address. TURN settings are shared with
voice through `WEBRTC_ICE_*`.

## Recovery observability

Backend logs expose monotonic per-process totals without logging SDP, ICE
credentials, or pixels:

- `event=stream_media_recovery_started` when a stream session enters bounded
  ICE recovery;
- `event=stream_media_recovery_succeeded` when that session reconnects;
- `event=stream_media_recovery_exhausted` before the failed session is removed;
- `event=websocket_outgoing_queue_full` once per affected connection when its
  bounded 128-event queue cannot accept another event.

Alert on an increasing exhaustion/start ratio and any sustained queue-full
rate. Totals reset on process restart, so a log collector should convert them
to external counters if long-term aggregation is needed.

## Browser and network smoke

The frontend Playwright suite uses two real headless Chromium contexts, fake
microphones and an animated synthetic screen track. Both prepared users must be
members of the same server and voice channel. The suite logs in, joins voice,
publishes through the SFU, verifies decoded frames, forces a network break, and
requires a new remote ICE generation with decoded frames after recovery.

```env
VOXHOLD_E2E_BASE_URL=https://test.example.com
VOXHOLD_E2E_VOICE_CHANNEL=e2e-voice
VOXHOLD_E2E_PUBLISHER_USERNAME=e2e-publisher
VOXHOLD_E2E_PUBLISHER_PASSWORD=change-me
VOXHOLD_E2E_VIEWER_USERNAME=e2e-viewer
VOXHOLD_E2E_VIEWER_PASSWORD=change-me
```

Run it from the frontend repository with `npm run test:e2e:stream`. To include
the relay assertion, block the direct WebRTC UDP path for the browser runner,
configure a reachable TURN server, and set `VOXHOLD_E2E_RUN_TURN=1`. The test
then requires the nominated remote candidate to be `relay`. Trace, video,
screenshot, WebRTC frame counts, ICE generations, and candidate types are kept
only for failed runs.

Before enabling automatic HDR, separately run the physical Windows HDR
on/off × HDR/SDR display × supported Chromium × AV1/VP9 matrix. For each case,
record join time, freeze ratio, recovery success, CPU/GPU, memory and upload for
1/8/32 viewers under 1/3/5% loss, jitter, reorder, and UDP-blocked TURN. Also
perform the deploy-owned backup restore drill. A headless synthetic SDR smoke
cannot certify 10-bit display output or visual tone-map accuracy.
