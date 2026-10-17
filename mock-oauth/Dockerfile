# syntax=docker/dockerfile:1.6

FROM oven/bun:1.4.3-alpine@sha256:629e17411f1f129dbec3af78d5af9c9f2a937435c80349437206c6b0b7422373 AS base

RUN apk add --no-cache curl

WORKDIR /app

FROM base AS deps

COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

FROM deps AS build

COPY tsconfig.json tsconfig.build.json ./
COPY src ./src
RUN bun run build

FROM base AS prod-deps

COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production

FROM base AS runtime

ARG MOCK_OAUTH_PORT=3345
ENV MOCK_OAUTH_PORT=${MOCK_OAUTH_PORT}

COPY --from=prod-deps /app/node_modules ./node_modules
COPY --from=prod-deps /app/package.json ./package.json
COPY --from=prod-deps /app/bun.lock ./bun.lock
COPY --from=build /app/dist ./dist

EXPOSE ${MOCK_OAUTH_PORT}

USER bun

RUN bun dist/server.js --help

CMD ["bun", "dist/server.js"]
