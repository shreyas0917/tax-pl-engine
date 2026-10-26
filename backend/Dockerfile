# syntax=docker/dockerfile:1
# Multi-stage build: one image serves as api, worker and migrate (see docker-compose.yml).

FROM golang:1.26-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/taxpl ./cmd/taxpl && \
    CGO_ENABLED=0 GOOS=linux go install github.com/pressly/goose/v3/cmd/goose@v3.24.3 && \
    cp /go/bin/goose /out/goose

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget && adduser -D -u 10001 taxpl
WORKDIR /app
COPY --from=builder /out/api /out/worker /out/taxpl /out/goose /usr/local/bin/
COPY --from=builder /src/db/migrations /app/db/migrations
RUN mkdir -p /app/data/blobs && chown -R taxpl:taxpl /app
USER taxpl
ENV HTTP_ADDR=:8080 BLOB_DRIVER=local BLOB_LOCAL_DIR=/app/data/blobs
EXPOSE 8080
CMD ["api"]
