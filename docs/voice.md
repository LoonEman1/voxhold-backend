# Voice protocol

Voxhold uses the authenticated `/api/v1/ws` connection for voice state
and WebRTC signaling. Audio packets never travel through the WebSocket.
They use a WebRTC connection to the server and are relayed to the other
participants as Opus RTP. The same connection supports an optional VP8 webcam
track; see [webcams](webcam.md). Screen sharing keeps its separate connection.

## Join flow

After the regular `auth` / `ready` exchange, send:

```json
{
  "request_id": "voice-join-1",
  "type": "voice.join",
  "data": {
    "server_id": 1,
    "channel_id": 2,
    "self_mute": false,
    "self_deaf": false
  }
}
```

The channel must exist, have kind `voice`, and be accessible to the user.
The server replies with `voice.joined`, followed by a
`voice.webrtc_offer` containing an SDP offer.

Before answering, the client creates an `RTCPeerConnection`, requests the
microphone, and adds the microphone track. It applies the server offer,
creates an answer, sets that answer as its local description, and sends:

```json
{
  "request_id": "voice-answer-1",
  "type": "voice.webrtc_answer",
  "data": {
    "sdp": "..."
  }
}
```

The server confirms it with `voice.webrtc_answered`.

Both directions use `voice.ice_candidate` for trickle ICE:

```json
{
  "type": "voice.ice_candidate",
  "data": {
    "candidate": "candidate:...",
    "sdp_mid": "0",
    "sdp_mline_index": 0,
    "username_fragment": "..."
  }
}
```

The client must buffer remote ICE candidates received before it has applied
an SDP offer. The server performs the same buffering for client candidates.

When another microphone track appears or disappears, the server sends a new
`voice.webrtc_offer`. The client must answer every new offer using the same
flow. Incoming remote audio is delivered through the browser
`RTCPeerConnection.ontrack` callback.

## Voice state

The server sends `voice.snapshot` after WebSocket authentication. Server
members receive these events when voice state changes:

- `voice.participant_joined`
- `voice.state_updated`
- `voice.participant_left`

Participants in snapshots and state events may have `reconnecting: true`
while WebRTC is recovering. After an unexpected WebSocket disconnect, the
server retains the participant in the roster for up to one minute. A client
rejoining the same channel can send its previous `connection_id` as
`resume_connection_id` in `voice.join`. Observers receive
`voice.participant_left` with reason `reconnected` for the old connection,
followed by `voice.state_updated` for the replacement connection.

Mute or deafen the current connection with:

```json
{
  "request_id": "voice-state-1",
  "type": "voice.state_update",
  "data": {
    "self_mute": true,
    "self_deaf": false
  }
}
```

`self_mute` makes the server discard that connection's incoming RTP.
`self_deaf` removes the other participants' outgoing tracks from that
connection and triggers renegotiation.

Leave with `voice.leave`. Disconnecting the WebSocket, leaving or being
kicked from the server, deleting the channel, and deleting the server also
close the WebRTC session automatically.

### Voice moderation

Send `voice.moderate` with the participant's `server_id`, `channel_id`,
`user_id`, and `connection_id`. Set `action` to one of the following.

| Action | Additional field | Role permission |
| --- | --- | --- |
| `mute` | `muted` | 32 |
| `move` | `destination_channel_id` | 64 |
| `kick` | | 128 |

Owners and admins have these permissions by default. Owners can moderate
admins. Custom roles can grant the permissions for moderating members.
The server acknowledges accepted requests with `voice.moderated`.
A server mute drops the participant's microphone audio until lifted and
persists across voice joins on that server. Clients receive it separately
from `self_mute` as `server_mute`.

Kick sends `voice.left` with reason `kicked` to the participant, who can
join again manually. Move requires destination access for both users and
a participant that sent `supports_move: true` in `voice.join`. It sends
`voice.left` with reason `moved` and the `destination_channel_id`, which the
client uses to join with the destination's media and encryption settings.

## Deployment

The SFU shares one UDP port across all peer connections:

```dotenv
WEBRTC_UDP_PORT=50000
WEBRTC_MAX_PARTICIPANTS=32
WEBRTC_MAX_AUDIO_BITRATE_KBPS=128
WEBRTC_PUBLIC_IP=127.0.0.1
```

`WEBRTC_MAX_AUDIO_BITRATE_KBPS` limits each microphone to at most 128
Kbit/s. The limit is advertised through Opus SDP and enforced against the
actual incoming Opus payload with a short burst allowance.

Use `127.0.0.1` only when the client runs on the same machine. On a VPS,
set `WEBRTC_PUBLIC_IP` to its public IP and allow the configured UDP port in
the firewall. `WEBRTC_ICE_SERVERS`, `WEBRTC_ICE_USERNAME`, and
`WEBRTC_ICE_CREDENTIAL` optionally configure STUN/TURN.

WebRTC encrypts each client-to-server media connection with DTLS-SRTP. The
server sees RTP because it must relay it, but the audio is encrypted while
travelling over the network.
