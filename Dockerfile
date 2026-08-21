ARG GO_VERSION=1.26.5

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

RUN mkdir -p /out && \
    CGO_ENABLED=0 GOBIN=/out go install \
    -tags sqlite \
    github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -o /out/voxhold \
    ./cmd/api

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -o /out/voxhold-bootstrap \
    ./cmd/bootstrap


FROM alpine:3.22

WORKDIR /app

LABEL org.opencontainers.image.source="https://github.com/LoonEman1/voxhold-backend"
LABEL org.opencontainers.image.licenses="AGPL-3.0-only"

RUN addgroup -S -g 10001 voxhold && \
    adduser -S -D -H -u 10001 -G voxhold voxhold && \
    mkdir -p /app/data && \
    chown -R voxhold:voxhold /app

COPY --from=builder /out/voxhold /usr/local/bin/voxhold
COPY --from=builder /out/voxhold-bootstrap /usr/local/bin/voxhold-bootstrap
COPY --from=builder /out/migrate /usr/local/bin/migrate
COPY migrations /app/migrations
COPY LICENSE /usr/share/licenses/voxhold-backend/LICENSE

USER 10001:10001

EXPOSE 8080/tcp
EXPOSE 50000/udp
EXPOSE 50001/udp

CMD ["voxhold"]
