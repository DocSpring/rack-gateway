import cors from "cors";
import express, { type Request, type Response } from "express";
import { exportJWK, generateKeyPair, type JWK, SignJWT } from "jose";
import { v4 as uuidv4 } from "uuid";

type MockUser = {
  id: string;
  email: string;
  name: string;
  picture: string;
  verified_email: boolean;
};

type AuthorizationCodePayload = {
  clientId: string;
  redirectUri: string;
  codeChallenge?: string;
  codeChallengeMethod?: string;
  user: MockUser;
  expires: number;
};

type AccessTokenPayload = {
  user: MockUser;
  expires: number;
};

type TokenRequestBody = {
  grant_type?: string;
  code?: string;
  redirect_uri?: string;
  code_verifier?: string;
  client_id?: string;
};

type PublicJwk = JWK & { kid: string };
type SigningContext = {
  publicJwk: PublicJwk;
  signingKey: CryptoKey;
  kid: string;
};

const mockUsers: Record<string, MockUser> = {
  "admin@example.com": {
    id: "1",
    email: "admin@example.com",
    name: "Admin User",
    picture: "https://via.placeholder.com/128",
    verified_email: true,
  },
  "deployer@example.com": {
    id: "2",
    email: "deployer@example.com",
    name: "Deployer User",
    picture: "https://via.placeholder.com/128",
    verified_email: true,
  },
  "ops@example.com": {
    id: "4",
    email: "ops@example.com",
    name: "Ops User",
    picture: "https://via.placeholder.com/128",
    verified_email: true,
  },
  "viewer@example.com": {
    id: "3",
    email: "viewer@example.com",
    name: "Viewer User",
    picture: "https://via.placeholder.com/128",
    verified_email: true,
  },
};

const authCodes = new Map<string, AuthorizationCodePayload>();
const accessTokens = new Map<string, AccessTokenPayload>();

const HTTP_STATUS = {
  badRequest: 400,
  unauthorized: 401,
  internalServerError: 500,
  ok: 200,
} as const;

const ONE_MINUTE_IN_MS = 60_000;
const ONE_HOUR_IN_MS = 60 * ONE_MINUTE_IN_MS;
const AUTH_CODE_TTL = 10 * ONE_MINUTE_IN_MS;

const showHelp = (): void => {
  console.log(
    "Mock OAuth Server\n\nUsage: node dist/server.js [options]\n\nOptions:\n  --help, -h       Show this help message and exit.\n\nConfiguration:\n  MOCK_OAUTH_PORT    Port the HTTP server listens on (default: 3345).\n  PORT               Fallback port if MOCK_OAUTH_PORT not set.\n  OAUTH_ISSUER       Internal issuer URL override.\n  OAUTH_BROWSER_BASE Browser-facing base URL override.\n"
  );
};

const argv = process.argv.slice(2);
if (argv.includes("--help") || argv.includes("-h")) {
  showHelp();
  process.exit(0);
}

const app = express();
const PORT = Number.parseInt(process.env.MOCK_OAUTH_PORT ?? process.env.PORT ?? "3345", 10);

app.use(cors());
app.use(express.json());
app.use(express.urlencoded({ extended: true }));

const resolveInternalBase = (req: Request): string =>
  process.env.OAUTH_ISSUER ?? `${req.protocol}://${req.get("host")}`;

const resolveBrowserBase = (req: Request): string =>
  process.env.OAUTH_BROWSER_BASE ?? `${req.protocol}://${req.get("host")}`;

let publicJwk: PublicJwk | undefined;
let currentKid: string | undefined;
let signingKey: CryptoKey | undefined;

const getSigningContext = (): SigningContext => {
  if (!(publicJwk && signingKey && currentKid)) {
    throw new Error("Signing keys have not been initialised");
  }
  return {
    publicJwk,
    signingKey,
    kid: currentKid,
  };
};

const initializeKeys = async (): Promise<void> => {
  const { publicKey, privateKey } = await generateKeyPair("RS256");
  const jwk = await exportJWK(publicKey);
  const kid = uuidv4();
  publicJwk = {
    ...jwk,
    kty: "RSA",
    kid,
    use: "sig",
    alg: "RS256",
  } as PublicJwk;
  signingKey = privateKey;
  currentKid = kid;
};

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

  const redirectUrl = new URL(redirect_uri);
  redirectUrl.searchParams.set("code", authCode);
  if (state) {
    redirectUrl.searchParams.set("state", state);
  }

  return res.redirect(redirectUrl.toString());
});

app.post("/oauth2/v4/token", async (req: Request, res: Response) => {
  const body = req.body as TokenRequestBody;
  const { grant_type, code, redirect_uri } = body;

  let clientId = body.client_id;
  const authHeader = req.headers.authorization ?? "";

  if (authHeader.startsWith("Basic ")) {
    try {
      const decoded = Buffer.from(authHeader.slice(6), "base64").toString("utf8");
      const [user] = decoded.split(":", 2);
      clientId = user;
    } catch (error) {
      console.warn("Failed to decode Basic auth header", error);
    }
  }

  if (grant_type !== "authorization_code") {
    return res.status(HTTP_STATUS.badRequest).json({
      error: "unsupported_grant_type",
      error_description: "Only authorization_code grant type is supported",
    });
  }

  if (!(code && redirect_uri && clientId)) {
    return res.status(HTTP_STATUS.badRequest).json({
      error: "invalid_request",
      error_description: "Missing required parameters",
    });
  }

  const authData = authCodes.get(code);
  if (!authData || authData.expires < Date.now()) {
    return res.status(HTTP_STATUS.badRequest).json({
      error: "invalid_grant",
      error_description: "Invalid or expired authorization code",
    });
  }

  if (authData.redirectUri !== redirect_uri || authData.clientId !== clientId) {
    return res.status(HTTP_STATUS.badRequest).json({
      error: "invalid_grant",
      error_description: "Redirect URI or client ID mismatch",
    });
  }

  const accessToken = uuidv4();
  const idToken = await generateMockIdToken(authData.user);

  accessTokens.set(accessToken, {
    user: authData.user,
    expires: Date.now() + ONE_HOUR_IN_MS,
  });

  authCodes.delete(code);

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

type UserSelectionParams = {
  client_id?: string;
  redirect_uri?: string;
  response_type?: string;
  scope?: string;
  state?: string;
  code_challenge?: string;
  code_challenge_method?: string;
};

app.get(
  "/dev/select-user",
  (req: Request<unknown, unknown, unknown, UserSelectionParams>, res: Response) => {
    const users = Object.values(mockUsers);
    const query = req.query ?? {};
    const queryParams = new URLSearchParams(query as Record<string, string>);

    const cardsHtml = users
      .map(
        (user) => `
        <div class="user-card" data-email="${user.email}">
          <h3>${user.name}</h3>
          <p class="email">${user.email}</p>
        </div>
      `
      )
      .join("");

    res.type("html").send(`
    <!DOCTYPE html>
    <html>
      <head>
        <meta charset="utf-8" />
        <title>Mock OAuth - Select User</title>
        <style>
          :root { color-scheme: dark; }
          body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial,
              'Noto Sans', 'Apple Color Emoji', 'Segoe UI Emoji';
            margin: 40px;
            background-color: #09090b;
            color: #fafafa;
          }
          .container { max-width: 720px; margin: 0 auto; }
          .muted { color: #a1a1aa; }
          .user-card {
            border: 1px solid #27272a;
            background: #111113;
            padding: 16px 20px;
            margin: 12px 0;
            border-radius: 8px;
            cursor: pointer;
            transition: background-color .15s ease, border-color .15s ease;
          }
          .user-card:hover { background-color: #18181b; border-color: #3f3f46; }
          h1 { font-size: 22px; margin: 0 0 6px; }
          p.sub { margin: 0 0 12px; }
          h3 { margin: 0 0 4px; font-size: 16px; }
          p.email { font-size: 13px; color: #a1a1aa; margin: 0; }
        </style>
      </head>
      <body>
        <div class="container">
          <h1>Mock Google OAuth – Select User</h1>
          <p class="sub muted">Choose which user to authenticate as:</p>
          ${cardsHtml}
        </div>
        <script>
          const params = new URLSearchParams(${JSON.stringify(queryParams.toString())});
          document.querySelectorAll('.user-card').forEach((card) => {
            card.addEventListener('click', () => {
              const email = card.getAttribute('data-email');
              if (!email) return;
              params.set('selected_user', email);
              sessionStorage.setItem('selectedUser', email);
              window.location.href = '/oauth2/v2/auth?' + params.toString();
            });
          });
        </script>
      </body>
    </html>
  `);
  }
);

const generateMockIdToken = (user: MockUser): Promise<string> => {
  const { signingKey: signingKeyContext, kid } = getSigningContext();
  const issuer = process.env.OAUTH_ISSUER ?? `http://localhost:${PORT}`;
  const now = Math.floor(Date.now() / 1000);

  const payload = {
    iss: issuer,
    sub: user.id,
    aud: "mock-client-id",
    exp: now + 3600,
    iat: now,
    email: user.email,
    email_verified: user.verified_email,
    name: user.name,
    picture: user.picture,
  };

  return new SignJWT(payload)
    .setProtectedHeader({ alg: "RS256", kid })
    .setIssuer(issuer)
    .setAudience("mock-client-id")
    .setIssuedAt()
    .setExpirationTime("1h")
    .sign(signingKeyContext);
};

app.get("/health", (_req: Request, res: Response) => {
  res.json({ status: "healthy", timestamp: new Date().toISOString() });
});

app.get("/", (_req: Request, res: Response) => {
  res.json({
    name: "Mock Google OAuth Server",
    version: "1.0.0",
    endpoints: {
      discovery: "/.well-known/openid_configuration",
      authorization: "/oauth2/v2/auth",
      token: "/oauth2/v4/token",
      userinfo: "/oauth2/v2/userinfo",
      dev_user_selector: "/dev/select-user",
      health: "/health",
    },
    mock_users: Object.keys(mockUsers),
    environment: process.env.NODE_ENV ?? "development",
  });
});

void initializeKeys()
  .then(() => {
    app.listen(PORT, "0.0.0.0", () => {
      console.log(`Mock OAuth server running on port ${PORT}`);
      console.log(`Visit http://localhost:${PORT} for endpoint info`);
      console.log("Mock users available:", Object.keys(mockUsers).join(", "));
    });
  })
  .catch((error) => {
    console.error("Failed to initialise signing keys", error);
    process.exitCode = 1;
  });

export default app;
