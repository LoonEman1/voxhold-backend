# Security policy

## Supported versions

Voxhold backend does not currently maintain multiple release branches.

| Version | Security fixes |
| --- | --- |
| Current `main` and the newest published container image | Supported |
| Older commits, images and forks | Not supported |

When reporting an issue, include the exact Git commit, SemVer image tag or OCI
digest. The mutable `latest` tag alone is not enough to identify a build.

## Reporting a vulnerability

Do **not** open a public issue, discussion or pull request for a suspected
vulnerability.

Use GitHub's private vulnerability reporting page when it is enabled:

https://github.com/LoonEman1/voxhold-backend/security/advisories/new

If that page is unavailable, open a public issue containing only the title
`Private security contact requested` and ask the maintainer to arrange a secure
channel. Do not include exploit details, credentials or private user data in
that issue.

Include, when applicable:

- affected commit, image tag and digest;
- deployment topology and relevant configuration with secrets removed;
- impact and the access level required to reproduce it;
- minimal reproduction steps or proof of concept;
- sanitized logs, requests and responses;
- whether the issue is already being exploited or publicly known;
- a safe way to contact you for follow-up.

Never send real passwords, bearer tokens, invite tokens, `.env` files, SQLite
databases or personal user content. Replace them with synthetic values.

## What to expect

Reports are handled on a best-effort basis; there is currently no guaranteed
response-time SLA. The maintainer will try to:

1. acknowledge a complete report;
2. reproduce and assess the impact;
3. coordinate a fix and container rebuild;
4. agree on disclosure timing with the reporter;
5. credit the reporter if requested and appropriate.

Please allow reasonable time for investigation and remediation before public
disclosure. Avoid accessing, changing or retaining data that does not belong to
you, degrading a third-party service, or using social engineering.

## Scope

This policy covers code and official container images from this repository:

- HTTP API and authentication;
- WebSocket realtime transport and signaling;
- WebRTC voice and streaming server paths;
- SQLite storage, migrations and bootstrap;
- the official backend Dockerfile and GitHub Actions workflow.

Issues found only in the official website, native client or deployment scripts
should be reported to their respective repositories. Vulnerabilities in a
third-party frontend or a modified deployment are outside this repository's
scope unless the root cause is in Voxhold backend.

## Security model notes

- TLS termination and host firewalling are deployment responsibilities.
- Server-relayed WebRTC media is encrypted in transit but is not end-to-end
  encrypted against the Voxhold server.
- Peer-to-peer streaming can reveal participant IP addresses to peers.
- In-process rate limits are local to one backend process and are not a
  distributed denial-of-service control.

See [docs/security.md](docs/security.md) for operational hardening guidance.
