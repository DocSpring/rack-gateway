# syntax=docker/dockerfile:1
FROM oven/bun:1.3.1-alpine AS webbuild

WORKDIR /app/web

# Copy web package and lockfile first to maximize cache hits
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile

# Copy web source and build
COPY web/ ./
RUN bun run build

FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates make gcc musl-dev

WORKDIR /app

# Cache go mod download
COPY go.mod go.sum ./
RUN go mod download

# Copy only the source needed to build the gateway
COPY internal ./internal
COPY cmd/gateway ./cmd/gateway

# Build the gateway binary directly in this stage
RUN CGO_ENABLED=0 go build -o /out/rack-gateway-api ./cmd/gateway \
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
