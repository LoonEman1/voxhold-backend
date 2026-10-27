# Media decisions for Voxhold 2.1

Status: retain the current architecture; record prerequisites for later changes.
Evidence: [audit report](audit-2.1.ru.md) and [raw measurements](audit-2.1/measurements.json).
Synthetic direct UDP forwarding reached ten peers, including nine video viewers.
These tests do not measure browser decoding, WAN congestion, TURN, or isolated SFU CPU.

## C04: stream adaptation

Retain the existing single-quality SDR stream and explicit HDR/SDR compatibility
selection. The baseline does not establish quality for a bandwidth-limited viewer.
Do not lower every viewer to the slowest viewer, or discard arbitrary dependent
video packets. A future adaptation protocol needs multiple spatial/bitrate levels
per dynamic range (the current contract permits one), per-viewer layer selection,
keyframe transitions and explicit fallback/version negotiation. Acceptance must
measure real decoded freezes and retransmissions at 2 Mbps / 100–200 ms / 1–3% loss,
while a good viewer keeps its quality. Rollback must preserve legacy single-SDR clients.
A mature external SFU remains an alternative if publisher layering or congestion
control cannot be implemented and maintained economically in this code.

## C05: NACK cache and buffer ownership

Keep history=2048 and copying enabled. The stream smoke fills outbound caches;
reported heap includes publisher/viewer clients as well as SFU, so it cannot isolate
cache cost. Before changing history, collect isolated heap/alloc_space profiles at
6/16 Mbps with actual RTT and loss, NACK/RTX counts and post-close heap stabilization.
A shared immutable payload cache must prove payload lifetime, per-peer SSRC/PT,
header-extension mapping, late retransmit and cleanup. Require a measured benefit
of at least 10% of the chosen resource metric; otherwise keep the simpler design.

## C06: small P2P streams

Keep the existing explicit P2P mode and viewer cap. No automatic threshold is chosen.
A future comparison needs 1/2/3/8 viewers, direct and TURN, publisher encode CPU and
uplink, infrastructure egress, decoded quality and NAT/privacy behavior. Switching
modes must preserve ACL, reconnect and stop semantics and have an explicit fallback.
Nine viewers do not fit the current P2P cap; raising it does not solve publisher uplink.

## C07: service separation and recording storage

Keep one API/media process and local private recordings for this release. There is
no measured isolated API/media contention establishing a migration benefit. An SFU
service would require room ownership/routing, authenticated session handoff,
revocation and reconnect coordination; generic round-robin replicas would lose
in-memory room state. A canary route and reversible room ownership are prerequisites.
Object storage/CDN is an independent option for ready recordings. Signed URLs must
retain the current download-ticket guarantee: recheck ACL when opening a download,
including tickets issued before revoke. Define cleanup/retention/quota and migration
rollback before moving files. Neither migration is justified solely by theoretical egress.
