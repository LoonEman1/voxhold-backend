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
video track in the declared codec and at most one Opus audio track. Pending ICE
candidates are capped at 64 per server-side media session. The common WebSocket
event limit also caps SDP and P2P signaling payloads.

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
