# syntax=docker/dockerfile:1
FROM oven/bun:1.4.4-alpine@sha256:4cf82f5686b196504fe119db3fcd9185b191f82ece82c40680e597228ec6070a AS webbuild

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

FROM golang:1.27.3-alpine@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS builder

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
# Release images are built without build tags. The E2E and local stacks pass GO_BUILD_TAGS=testbypass to compile in
# the test-only bypasses (DEV_MODE, E2E_TEST_MODE, endpoint overrides); a release binary refuses to start with them.
ARG GO_BUILD_TAGS=""
RUN VERSION=$(node -p "require('./web/package.json').version") && \
    CGO_ENABLED=0 go build -tags "${GO_BUILD_TAGS}" \
    -ldflags "-X github.com/DocSpring/rack-gateway/internal/gateway/version.Version=${VERSION} -X github.com/DocSpring/rack-gateway/internal/gateway/version.CommitHash=${COMMIT_SHA}" \
    -o /out/rack-gateway-api ./cmd/gateway \
    && /out/rack-gateway-api build-info

FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

# ca-certificates for outbound TLS. No curl: the compose healthcheck uses busybox wget.
# /app/certs is made here because COPY --chmod would give a directory it creates the file's mode, which leaves no
# search bit for the runtime user.
RUN apk --no-cache add ca-certificates \
    && addgroup -S -g 10001 gateway \
    && adduser -S -D -H -u 10001 -G gateway -s /sbin/nologin gateway \
    && mkdir -p /app/certs

WORKDIR /app

# Files stay root-owned and read-only to the runtime user.
COPY --from=builder /out/rack-gateway-api ./
COPY --from=webbuild /app/web/dist ./web/dist
COPY --chmod=0755 scripts/start-gateway.sh ./scripts/start-gateway.sh
# Amazon RDS's CA bundle, for PGSSLMODE=verify-full with PGSSLROOTCERT (see certs/README.md)
COPY --chmod=0644 certs/rds-global-bundle.pem ./certs/rds-global-bundle.pem

USER 10001:10001

EXPOSE 8080

CMD ["./rack-gateway-api"]
