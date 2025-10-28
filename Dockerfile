FROM oven/bun:1.3.1-alpine AS webbuilder

WORKDIR /webapp

# Install deps with maximum cache reuse: copy only lockfile + manifest first
COPY web/bun.lock web/package.json ./
RUN bun install --frozen-lockfile

# Copy the rest of the web app and build
COPY web/ ./
RUN bunx tsc -b tsconfig.build.json \
    && bunx vite build --base=/web/

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

WORKDIR /root/

COPY --from=builder /out/rack-gateway-api .
COPY config ./config
COPY --from=webbuilder /webapp/dist ./web/dist

EXPOSE 8080

CMD ["./rack-gateway-api"]
