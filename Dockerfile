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


FROM alpine:3.22

WORKDIR /app

COPY --from=builder /out/voxhold /usr/local/bin/voxhold
COPY --from=builder /out/migrate /usr/local/bin/migrate
COPY migrations /app/migrations

EXPOSE 8080/tcp
EXPOSE 50000/udp

CMD ["voxhold"]
