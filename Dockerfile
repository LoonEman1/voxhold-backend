ARG GO_VERSION=1.27.3

# The builder always runs natively and cross-compiles for the target platform:
# the binaries are pure Go (CGO_ENABLED=0), so ARM64 images no longer compile
# under QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
# Each image contains one CPU variant. Published portable tags use v1.
ARG GOAMD64=v1
ARG PGO=auto

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

# go install refuses GOBIN for cross-compiled binaries; it then writes them to
# $GOPATH/bin/<os>_<arch> instead of $GOPATH/bin.
RUN mkdir -p /out && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOAMD64=v1 go install \
    -tags sqlite \
    github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1 && \
    bin="$(go env GOPATH)/bin" && \
    if [ -f "$bin/${TARGETOS}_${TARGETARCH}/migrate" ]; then \
        cp "$bin/${TARGETOS}_${TARGETARCH}/migrate" /out/migrate; \
    else \
        cp "$bin/migrate" /out/migrate; \
    fi

COPY . .

RUN GOARCH=${TARGETARCH} GOAMD64=${GOAMD64} PGO=${PGO} sh scripts/build-linux.sh /out


FROM alpine:3.24

WORKDIR /app

LABEL org.opencontainers.image.source="https://github.com/LoonEman1/voxhold-backend"
LABEL org.opencontainers.image.licenses="AGPL-3.0-only"
LABEL io.voxhold.cpu-check="1"

RUN addgroup -S -g 10001 voxhold && \
    adduser -S -D -H -u 10001 -G voxhold voxhold && \
    mkdir -p /app/data && \
    chown -R voxhold:voxhold /app

COPY --from=builder /out/voxhold /usr/local/bin/voxhold
COPY --from=builder /out/voxhold-bootstrap /usr/local/bin/voxhold-bootstrap
COPY --from=builder /out/voxhold-admin /usr/local/bin/voxhold-admin
COPY --from=builder /out/migrate /usr/local/bin/migrate
COPY migrations /app/migrations
COPY LICENSE /usr/share/licenses/voxhold-backend/LICENSE

USER 10001:10001

EXPOSE 8080/tcp
EXPOSE 50000/udp
EXPOSE 50001/udp

CMD ["voxhold"]
