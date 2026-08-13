# Deployment security

The API has an in-process anti-abuse guard. It limits requests by client IP,
applies stricter limits to authentication and invite-link endpoints, limits
WebSocket connections and events, and temporarily blocks repeated invalid
login attempts. The limits are configured through the `HTTP_*`, `AUTH_*`,
`INVITE_*`, `LOGIN_*`, and `WS_*` variables in `.env`.

`TRUST_PROXY_HEADERS` is disabled by default. Enable it only when the API is
reachable exclusively through a trusted reverse proxy that overwrites
`X-Real-IP`; otherwise a client can spoof its address and bypass IP limits.

The runtime image runs as UID/GID 10001, drops Linux capabilities, enables
`no-new-privileges`, uses a read-only root filesystem, and keeps only the
SQLite volume writable. The three services share an internal Docker network.

For a VPS, put Caddy or Nginx in front of the API and terminate TLS there:

1. Set `HTTP_BIND_ADDRESS=127.0.0.1` so the HTTP port is not public.
2. Keep `HTTP_LISTEN_ADDRESS=0.0.0.0` because it is the address inside the
   container.
3. Proxy both HTTP and WebSocket traffic to `http://127.0.0.1:8080`.
4. Expose only TCP 80/443 and the configured WebRTC UDP ports in the VPS
   firewall. Do not expose the SQLite volume or Docker daemon.
5. Keep `RESET_DATABASE=false` and use a strong, private bootstrap password.

If an existing named volume was created by an older root container, it may
need a one-time ownership fix before enabling the non-root image:

```sh
docker run --rm -v voxhold-backend_voxhold_data:/app/data alpine \
  chown -R 10001:10001 /app/data
```

The application is intentionally not responsible for TLS, host firewalling,
SSH hardening, or distributed rate limiting. On multiple API replicas, put a
rate-limiting reverse proxy/WAF in front or use a shared limiter.
