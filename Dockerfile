FROM node:20-alpine AS webbuilder

WORKDIR /webapp

# Enable corepack to use pnpm
RUN corepack enable

# Copy web app and install deps
COPY web ./
RUN pnpm install --frozen-lockfile \
  && pnpm build -- --base=/.gateway/web/

FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates make gcc musl-dev sqlite-dev

WORKDIR /app

# Cache go mod download
COPY go.mod go.sum ./
RUN go mod download

# Copy only the source needed to build the gateway
COPY internal ./internal
COPY cmd/gateway ./cmd/gateway

# Build the gateway binary directly (avoid Makefile dependency)
RUN CGO_ENABLED=1 go build -o /out/convox-gateway-api ./cmd/gateway \
  && ./cmd/gateway help

FROM alpine:latest

RUN apk --no-cache add ca-certificates curl

WORKDIR /root/

COPY --from=builder /out/convox-gateway-api .
COPY config ./config
COPY --from=webbuilder /webapp/dist ./web/dist

EXPOSE 8080

CMD ["./convox-gateway-api"]
