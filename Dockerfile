# syntax=docker/dockerfile:1
FROM oven/bun:1.4.2-alpine@sha256:d888c0ae6c86d7866ff10c5aafdd9077b36aee6455b33dd270fb93c0dd5cef6f AS webbuild

WORKDIR /app/web

# Copy web package and lockfile first to maximize cache hits
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile

# Copy web source and build. COMMIT_SHA changes on every commit, so it comes after the install.
COPY web/ ./
ARG COMMIT_SHA
RUN test -n "$COMMIT_SHA" || (echo "COMMIT_SHA build arg is required" && exit 1)
ENV COMMIT_SHA=${COMMIT_SHA}
RUN bun run build

FROM golang:1.27.0-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS builder

RUN apk add --no-cache git ca-certificates make gcc musl-dev nodejs npm

WORKDIR /app

# Cache go mod download
COPY go.mod go.sum ./
COPY internal/shims ./internal/shims
RUN go mod download

# Copy version source (package.json for version number)
COPY web/package.json ./web/package.json

# Copy only the source needed to build the gateway
COPY internal ./internal
COPY cmd/gateway ./cmd/gateway

# Build the gateway binary with version info (COMMIT_SHA is the same build arg the web stage requires)
ARG COMMIT_SHA
RUN test -n "$COMMIT_SHA" || (echo "COMMIT_SHA build arg is required" && exit 1)
RUN VERSION=$(node -p "require('./web/package.json').version") && \
    CGO_ENABLED=0 go build \
    -ldflags "-X github.com/DocSpring/rack-gateway/internal/gateway/version.Version=${VERSION} -X github.com/DocSpring/rack-gateway/internal/gateway/version.CommitHash=${COMMIT_SHA}" \
    -o /out/rack-gateway-api ./cmd/gateway \
    && /out/rack-gateway-api help

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

# ca-certificates for outbound TLS. No curl: the compose healthcheck uses busybox wget.
RUN apk --no-cache add ca-certificates \
    && addgroup -S -g 10001 gateway \
    && adduser -S -D -H -u 10001 -G gateway -s /sbin/nologin gateway

WORKDIR /app

# Files stay root-owned and read-only to the runtime user.
COPY --from=builder /out/rack-gateway-api ./
COPY --from=webbuild /app/web/dist ./web/dist
COPY --chmod=0755 scripts/start-gateway.sh ./scripts/start-gateway.sh

USER 10001:10001

EXPOSE 8080

CMD ["./rack-gateway-api"]
