export const HTTP_STATUS = {
  badRequest: 400,
  unauthorized: 401,
  internalServerError: 500,
  ok: 200,
} as const;

export const ONE_MINUTE_IN_MS = 60_000;
export const ONE_HOUR_IN_MS = 60 * ONE_MINUTE_IN_MS;
export const AUTH_CODE_TTL = 10 * ONE_MINUTE_IN_MS;
export const DEFAULT_PORT = 3345;

export const TOPICS = {
  http: "http",
  httpHeaders: "http.headers",
  httpBody: "http.body",
  flow: "flow",
  tokens: "tokens",
} as const;
