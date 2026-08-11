# Voice protocol

Voxhold uses the authenticated `/api/v1/ws` connection for voice state
and WebRTC signaling. Audio packets never travel through the WebSocket.
They use an audio-only WebRTC connection to the server and are relayed to
the other participants as Opus RTP.

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

## Deployment

The SFU shares one UDP port across all peer connections:

```dotenv
WEBRTC_UDP_PORT=50000
WEBRTC_MAX_PARTICIPANTS=32
WEBRTC_PUBLIC_IP=127.0.0.1
```

Use `127.0.0.1` only when the client runs on the same machine. On a VPS,
set `WEBRTC_PUBLIC_IP` to its public IP and allow the configured UDP port in
the firewall. `WEBRTC_ICE_SERVERS`, `WEBRTC_ICE_USERNAME`, and
`WEBRTC_ICE_CREDENTIAL` optionally configure STUN/TURN.

WebRTC encrypts each client-to-server media connection with DTLS-SRTP. The
server sees RTP because it must relay it, but the audio is encrypted while
travelling over the network.
