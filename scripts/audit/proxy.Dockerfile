FROM caddy:2.11.4 AS edge
FROM golang:1.27.3
COPY --from=edge /usr/bin/caddy /usr/local/bin/caddy
