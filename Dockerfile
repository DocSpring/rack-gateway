# syntax=docker/dockerfile:1
FROM oven/bun:1.3.2-alpine AS webbuild

ARG COMMIT_SHA
RUN test -n "$COMMIT_SHA" || (echo "COMMIT_SHA build arg is required" && exit 1)
ENV COMMIT_SHA=${COMMIT_SHA}

WORKDIR /app/web

# Copy web package and lockfile first to maximize cache hits
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile

# Copy web source and build
COPY web/ ./
RUN bun run build

FROM golang:1.25.7-alpine AS builder

RUN apk add --no-cache git ca-certificates make gcc musl-dev nodejs npm

WORKDIR /app

# Cache go mod download
COPY go.mod go.sum ./
RUN go mod download

# Copy version source (package.json for version number)
COPY web/package.json ./web/package.json

# Copy only the source needed to build the gateway
COPY internal ./internal
COPY cmd/gateway ./cmd/gateway

# Build the gateway binary with version info
ARG COMMIT_HASH=unknown
RUN VERSION=$(node -p "require('./web/package.json').version") && \
    CGO_ENABLED=0 go build \
    -ldflags "-X github.com/DocSpring/rack-gateway/internal/gateway/version.Version=${VERSION} -X github.com/DocSpring/rack-gateway/internal/gateway/version.CommitHash=${COMMIT_HASH}" \
    -o /out/rack-gateway-api ./cmd/gateway \
    && /out/rack-gateway-api help

FROM alpine:latest

RUN apk --no-cache add ca-certificates curl

WORKDIR /app

COPY --from=builder /out/rack-gateway-api ./
COPY --from=webbuild /app/web/dist ./web/dist
COPY scripts/start-gateway.sh ./scripts/start-gateway.sh
RUN chmod +x ./scripts/start-gateway.sh

EXPOSE 8080

CMD ["./rack-gateway-api"]
