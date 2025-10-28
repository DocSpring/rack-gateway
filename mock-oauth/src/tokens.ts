import type { Request } from "express";

import { HTTP_STATUS } from "./constants.js";
import { logger } from "./logger.js";
import { authCodes } from "./state.js";
import type { AuthorizationCodePayload, TokenRequestBody } from "./types.js";

export type ErrorResponse = {
  status: number;
  body: { error: string; error_description: string };
};

export type TokenValidationSuccess = {
  authData: AuthorizationCodePayload;
  clientId: string;
  code: string;
};

export type TokenValidationResult = TokenValidationSuccess | { error: ErrorResponse };

const badRequest = (error: string, description: string): ErrorResponse => ({
  status: HTTP_STATUS.badRequest,
  body: { error, error_description: description },
});

export const resolveClientId = (req: Request, body: TokenRequestBody): string | undefined => {
  let clientId = body.client_id;
  const authHeader = req.headers.authorization ?? "";

  if (authHeader.startsWith("Basic ")) {
    try {
      const decoded = Buffer.from(authHeader.slice(6), "base64").toString("utf8");
      const [user] = decoded.split(":", 2);
      clientId = user;
    } catch (error) {
      logger.warn("Failed to decode Basic auth header %o", error);
    }
  }

  return clientId;
};

export const validateTokenRequest = (
  req: Request,
  body: TokenRequestBody
): TokenValidationResult => {
  const { grant_type, code, redirect_uri } = body;

  if (grant_type !== "authorization_code") {
    return {
      error: badRequest(
        "unsupported_grant_type",
        "Only authorization_code grant type is supported"
      ),
    };
  }

  const clientId = resolveClientId(req, body);

  if (!(code && redirect_uri && clientId)) {
    return { error: badRequest("invalid_request", "Missing required parameters") };
  }

  const authData = authCodes.get(code);
  if (!authData || authData.expires < Date.now()) {
    logger.warn("Invalid or expired authorization code %s", code);
    return { error: badRequest("invalid_grant", "Invalid or expired authorization code") };
  }

  if (authData.redirectUri !== redirect_uri || authData.clientId !== clientId) {
    logger.warn(
      "Authorization code mismatch code=%s storedRedirect=%s providedRedirect=%s storedClient=%s providedClient=%s",
      code,
      authData.redirectUri,
      redirect_uri,
      authData.clientId,
      clientId
    );
    return { error: badRequest("invalid_grant", "Redirect URI or client ID mismatch") };
  }

  return { authData, clientId, code };
};
