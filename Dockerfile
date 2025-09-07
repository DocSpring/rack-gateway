# syntax=docker/dockerfile:1.5

FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates make gcc musl-dev sqlite-dev

WORKDIR /app

# Cache go mod download using BuildKit cache mounts
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    go mod download

# Copy only the source needed to build the gateway
COPY internal ./internal
COPY cmd/gateway ./cmd/gateway

# Build the gateway binary with BuildKit caches
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=1 go build -o /out/convox-gateway-api ./cmd/gateway

FROM alpine:latest

RUN apk --no-cache add ca-certificates curl

WORKDIR /root/

COPY --from=builder /out/convox-gateway-api .
COPY config ./config

EXPOSE 8080

CMD ["./convox-gateway-api"]
