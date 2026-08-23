# Operational security

For confidential vulnerability reports, follow the repository
[security policy](../SECURITY.md). This document covers deployment hardening;
it is not a substitute for host, Docker or reverse-proxy security.

## Recommended production topology

Use the separate
[`voxhold-deploy`](https://github.com/LoonEman1/voxhold-deploy) project for a
public instance. It runs migrations and bootstrap as one-shot jobs, keeps the
backend HTTP port inside the Compose network and exposes only Caddy on TCP
80/443. The configured WebRTC UDP ports remain public because media clients
must reach them directly.

The application itself intentionally does not manage TLS certificates, host
firewall rules, operating-system updates, SSH access or Docker daemon security.

For the standalone Compose file in this repository:

1. set `HTTP_BIND_ADDRESS=127.0.0.1` when a reverse proxy runs on the host;
2. keep `HTTP_LISTEN_ADDRESS=0.0.0.0`, which is the address inside the
   container;
3. proxy HTTP and WebSocket traffic to `http://127.0.0.1:8080`;
4. do not expose port 8080 directly to the Internet;
5. use the production deployment project instead when the reverse proxy also
   runs in Docker.

## Forwarded client addresses

The in-process abuse guard limits HTTP requests, authentication, invitations,
WebSocket connections and WebSocket events. Repeated invalid logins are
temporarily blocked. Limits are configured through `HTTP_*`, `AUTH_*`,
`INVITE_*`, `LOGIN_*` and `WS_*` variables.

`TRUST_PROXY_HEADERS=false` is the safe default when clients can reach the API
directly. Set it to `true` only when every request passes through a trusted
reverse proxy that overwrites client-supplied forwarding headers. Otherwise an
attacker can spoof an address and weaken IP-based limits.

The limiter is in-process. Multiple backend replicas need a shared limiter or
rate-limiting reverse proxy/WAF. These controls reduce abuse but do not replace
provider-level denial-of-service protection.

## Network and media

Allow only the ports required by the selected deployment:

- `80/tcp` and `443/tcp` for Caddy;
- `50000/udp` by default for voice;
- `50001/udp` by default for screen streaming;
- a restricted administrative SSH source range where possible.

Set `WEBRTC_PUBLIC_IP` to the server's reachable public address. Optional
STUN/TURN credentials belong in `.env`, never in the repository. TURN services
must have their own authentication, quotas and update policy.

WebRTC uses DTLS-SRTP on the network. Server-relayed voice and streaming media
is decrypted inside the SFU so it can be forwarded; it is not end-to-end
encrypted against the server. P2P streaming avoids the SFU media path but can
expose peer IP addresses and increases publisher upload usage. See
[voice.md](voice.md) and [streaming.md](streaming.md).

## Secrets and first bootstrap

- Keep `.env` readable only by the deployment administrator.
- Use a strong unique `BOOTSTRAP_PASSWORD`, or immediately save the generated
  password from the one-shot bootstrap logs.
- Treat Docker access and container logs as privileged: bootstrap logs may
  contain the generated owner password.
- Keep `RESET_DATABASE=false` outside an intentional local reset.
- Never commit bearer tokens, invite tokens, TURN credentials, databases or
  backups.
- Do not mount the Docker socket into Voxhold containers.

Registration is invite-only, but invite links are credentials. Transmit them
over HTTPS and choose the shortest practical expiration and use limit.

## Container and host hardening

The official runtime image executes as UID/GID `10001`. The provided Compose
configurations use a read-only root filesystem, drop Linux capabilities and
set `no-new-privileges`; only the SQLite data volume and explicit temporary
filesystems are writable.

Keep Docker Engine, the Linux kernel and reverse proxy updated. Prefer an exact
SemVer release tag such as `0.1.0`, or an OCI digest for an immutable reference,
over the moving `0.1` and `latest` tags. Review release changes and keep regular
off-host SQLite backups. Official CI publishes SBOM and provenance attestations
with the multi-platform image.

Do not run untrusted replacement frontend images on the same Docker host or
network. A container image is executable code, not merely static website
content.

## SQLite data

Protect the named volume and backups as production secrets. A backup should be
made while database writes are stopped or through the deployment backup script
so the database and its WAL state remain consistent. Test restoration
periodically.

If a volume was created by an older root container, repair its ownership once
before running the non-root image:

```bash
docker run --rm --user 0 \
  -v voxhold-backend_voxhold_data:/app/data \
  alpine:3.22 \
  sh -c 'chown -R 10001:10001 /app/data && chmod 770 /app/data'
```
