# syntax=docker/dockerfile:1.6

FROM node:20-alpine AS base

ENV PNPM_HOME=/pnpm
ENV PATH="$PNPM_HOME:$PATH"

RUN apk add --no-cache curl \
    && corepack enable

WORKDIR /app

FROM base AS deps

COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

FROM deps AS build

COPY tsconfig.json ./
COPY server.ts ./
RUN pnpm run build

FROM base AS prod-deps

COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --prod

FROM base AS runtime

ARG MOCK_OAUTH_PORT=3345
ENV MOCK_OAUTH_PORT=${MOCK_OAUTH_PORT}

COPY --from=prod-deps /app/node_modules ./node_modules
COPY --from=prod-deps /app/package.json ./package.json
COPY --from=prod-deps /app/pnpm-lock.yaml ./pnpm-lock.yaml
COPY --from=build /app/dist ./dist

EXPOSE ${MOCK_OAUTH_PORT}

USER node

RUN node dist/server.js --help

CMD ["node", "dist/server.js"]
