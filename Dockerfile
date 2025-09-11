FROM node:20-alpine AS webbuilder

WORKDIR /webapp

# Enable corepack to use pnpm
RUN corepack enable

# Install deps with maximum cache reuse: copy only lockfile + manifest first
COPY web/pnpm-lock.yaml web/package.json web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

# Copy the rest of the web app and build
COPY web/ ./
RUN pnpm build -- --base=/.gateway/web/

FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates make gcc musl-dev

WORKDIR /app

# Cache go mod download
COPY go.mod go.sum ./
RUN go mod download

# Copy only the source needed to build the gateway
COPY internal ./internal
COPY cmd/gateway ./cmd/gateway

# Build the gateway binary directly in this stage
RUN CGO_ENABLED=1 go build -o /out/convox-gateway-api ./cmd/gateway \
    && /out/convox-gateway-api help

FROM alpine:latest

RUN apk --no-cache add ca-certificates curl

WORKDIR /root/

COPY --from=builder /out/convox-gateway-api .
COPY config ./config
COPY --from=webbuilder /webapp/dist ./web/dist

EXPOSE 8080

CMD ["./convox-gateway-api"]
