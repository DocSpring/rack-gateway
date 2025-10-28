import type { Express, Request, Response } from "express";
import { v4 as uuidv4 } from "uuid";

import {
  AUTH_CODE_TTL,
  HTTP_STATUS,
  ONE_HOUR_IN_MS,
  ONE_MINUTE_IN_MS,
  TOPICS,
} from "./constants.js";
import { logger } from "./logger.js";
import { generateMockIdToken, getSigningContext } from "./signing.js";
import { accessTokens, authCodes } from "./state.js";
import { validateTokenRequest } from "./tokens.js";
import type { TokenRequestBody } from "./types.js";
import { renderUserSelectionPage } from "./user-selection-page.js";
import { mockUsers } from "./users.js";

const resolveInternalBase = (req: Request): string =>
  process.env.OAUTH_ISSUER ?? `${req.protocol}://${req.get("host")}`;

const resolveBrowserBase = (req: Request): string =>
  process.env.OAUTH_BROWSER_BASE ?? `${req.protocol}://${req.get("host")}`;

type UserSelectionParams = {
  client_id?: string;
  redirect_uri?: string;
  response_type?: string;
  scope?: string;
  state?: string;
  code_challenge?: string;
  code_challenge_method?: string;
};

export const registerRoutes = (app: Express): void => {
  app.get("/health", (_req: Request, res: Response) => {
    res.json({ status: "ok" });
  });

  app.get("/.well-known/jwks", (_req: Request, res: Response) => {
    const { publicJwk: currentPublicJwk } = getSigningContext();
    res.json({ keys: [currentPublicJwk] });
  });

  app.get("/.well-known/openid-configuration", (req: Request, res: Response) => {
    const internalBase = resolveInternalBase(req);
    const browserBase = resolveBrowserBase(req);
    res.json({
      issuer: internalBase,
      authorization_endpoint: `${browserBase}/oauth2/v2/auth`,
      token_endpoint: `${internalBase}/oauth2/v4/token`,
      userinfo_endpoint: `${internalBase}/oauth2/v2/userinfo`,
      jwks_uri: `${internalBase}/.well-known/jwks`,
      response_types_supported: ["code"],
      grant_types_supported: ["authorization_code"],
      subject_types_supported: ["public"],
      id_token_signing_alg_values_supported: ["RS256"],
      scopes_supported: ["openid", "email", "profile"],
    });
  });

  app.get("/oauth2/v2/auth", (req: Request, res: Response) => {
    const query = req.query as Record<string, string | undefined>;
    const {
      client_id,
      redirect_uri,
      response_type,
      state,
      code_challenge,
      code_challenge_method,
      selected_user,
    } = query;

    if (!(client_id && redirect_uri) || response_type !== "code") {
      return res.status(HTTP_STATUS.badRequest).json({
        error: "invalid_request",
        error_description: "Missing or invalid required parameters",
      });
    }

    if (!selected_user) {
      logger.debug(TOPICS.flow, "no user selected, redirecting to dev selector");
      return res.redirect(`/dev/select-user?${req.url.split("?")[1] ?? ""}`);
    }

    const user = mockUsers[selected_user];
    if (!user) {
      return res.status(HTTP_STATUS.badRequest).json({
        error: "invalid_user",
        error_description: "Selected user not found",
      });
    }

    const authCode = uuidv4();
    authCodes.set(authCode, {
      clientId: client_id,
      redirectUri: redirect_uri,
      codeChallenge: code_challenge,
      codeChallengeMethod: code_challenge_method,
      user,
      expires: Date.now() + AUTH_CODE_TTL,
    });

    logger.debug(TOPICS.flow, `issued auth code ${authCode} for ${user.email}`);

    const redirectUrl = new URL(redirect_uri);
    redirectUrl.searchParams.set("code", authCode);
    if (state) {
      redirectUrl.searchParams.set("state", state);
    }

    return res.redirect(redirectUrl.toString());
  });

  app.post("/oauth2/v4/token", async (req: Request, res: Response) => {
    const validation = validateTokenRequest(req, req.body as TokenRequestBody);
    if ("error" in validation) {
      return res.status(validation.error.status).json(validation.error.body);
    }

    const { authData, code } = validation;

    const accessToken = uuidv4();
    const idToken = await generateMockIdToken(authData.user);

    accessTokens.set(accessToken, {
      user: authData.user,
      expires: Date.now() + ONE_HOUR_IN_MS,
    });

    authCodes.delete(code);

    logger.debug(TOPICS.tokens, `issued access token for ${authData.user.email}`);

    return res.json({
      access_token: accessToken,
      token_type: "Bearer",
      expires_in: ONE_HOUR_IN_MS / ONE_MINUTE_IN_MS,
      id_token: idToken,
      scope: "openid email profile",
    });
  });

  app.get("/oauth2/v2/userinfo", (req: Request, res: Response) => {
    const authHeader = req.headers.authorization;
    if (!authHeader?.startsWith("Bearer ")) {
      return res.status(HTTP_STATUS.unauthorized).json({
        error: "invalid_token",
        error_description: "Invalid or missing access token",
      });
    }

    const token = authHeader.substring(7);
    const tokenData = accessTokens.get(token);

    if (!tokenData || tokenData.expires < Date.now()) {
      return res.status(HTTP_STATUS.unauthorized).json({
        error: "invalid_token",
        error_description: "Invalid or expired access token",
      });
    }

    return res.json(tokenData.user);
  });

  app.get(
    "/dev/select-user",
    (req: Request<unknown, unknown, unknown, UserSelectionParams>, res: Response) => {
      const users = Object.values(mockUsers);
      const query = req.query ?? {};
      const queryParams = new URLSearchParams(query as Record<string, string>);
      res.type("html").send(renderUserSelectionPage(users, queryParams.toString()));
    }
  );
};
