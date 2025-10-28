import type { AccessTokenPayload, AuthorizationCodePayload } from "./types.js";

export const authCodes = new Map<string, AuthorizationCodePayload>();
export const accessTokens = new Map<string, AccessTokenPayload>();
