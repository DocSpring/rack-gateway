const express = require("express");
const cors = require("cors");
const { v4: uuidv4 } = require("uuid");
const jose = require("jose");

const app = express();
const PORT = process.env.PORT || 3001;

// Enable CORS for all routes
app.use(cors());
app.use(express.json());
app.use(express.urlencoded({ extended: true }));

// Mock users database
const mockUsers = {
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

// Store authorization codes temporarily
const authCodes = new Map();
const accessTokens = new Map();

// Resolve internal and browser-facing bases
function getInternalBase(req) {
  return process.env.OAUTH_ISSUER || `${req.protocol}://${req.get("host")}`;
}

function getBrowserBase(req) {
  return (
    process.env.OAUTH_BROWSER_BASE || `${req.protocol}://${req.get("host")}`
  );
}

// In-memory signing key (RS256)
let jwkPublic;
let kid;
let privateKey;

async function initKeys() {
  const { publicKey, privateKey: priv } = await jose.generateKeyPair("RS256");
  privateKey = priv;
  const jwk = await jose.exportJWK(publicKey);
  jwk.kty = "RSA";
  kid = uuidv4();
  jwk.kid = kid;
  jwk.use = "sig";
  jwk.alg = "RS256";
  jwkPublic = jwk;
}

// JWKS endpoint for public keys
app.get("/.well-known/jwks", (_req, res) => {
  res.json({ keys: [jwkPublic] });
});

// OAuth discovery endpoint
app.get("/.well-known/openid-configuration", (req, res) => {
  const internalBase = getInternalBase(req);
  const browserBase = getBrowserBase(req);
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

// OAuth authorization endpoint with user selection
app.get("/oauth2/v2/auth", (req, res) => {
  const {
    client_id,
    redirect_uri,
    response_type,
    scope,
    state,
    code_challenge,
    code_challenge_method,
    selected_user,
  } = req.query;

  console.log("OAuth authorization request:", {
    client_id,
    redirect_uri,
    response_type,
    scope,
    state,
    selected_user,
  });

  // If no user selected, show user selection page
  if (!selected_user) {
    return res.redirect(`/dev/select-user?${req.url.split("?")[1]}`);
  }

  // Validate required parameters
  if (!client_id || !redirect_uri || response_type !== "code") {
    return res.status(400).json({
      error: "invalid_request",
      error_description: "Missing or invalid required parameters",
    });
  }

  const user = mockUsers[selected_user];
  if (!user) {
    return res.status(400).json({
      error: "invalid_user",
      error_description: "Selected user not found",
    });
  }

  // Generate authorization code
  const authCode = uuidv4();

  // Store auth code with associated data
  authCodes.set(authCode, {
    client_id,
    redirect_uri,
    code_challenge,
    code_challenge_method,
    user,
    expires: Date.now() + 600000, // 10 minutes
  });

  // Redirect back with code
  const redirectUrl = new URL(redirect_uri);
  redirectUrl.searchParams.set("code", authCode);
  if (state) redirectUrl.searchParams.set("state", state);

  res.redirect(redirectUrl.toString());
});

// OAuth token endpoint
app.post("/oauth2/v4/token", async (req, res) => {
  const { grant_type, code, redirect_uri, code_verifier } = req.body;

  // Allow client_id via Basic auth or request body
  let bodyClientId = req.body.client_id;
  let authClientId = null;
  const authHeader = req.headers.authorization || "";
  if (authHeader.startsWith("Basic ")) {
    try {
      const decoded = Buffer.from(authHeader.slice(6), "base64").toString(
        "utf8"
      );
      const [user, _pass] = decoded.split(":", 2);
      authClientId = user;
    } catch (_) {}
  }
  const client_id = bodyClientId || authClientId;

  console.log("OAuth token request:", {
    grant_type,
    code: code?.substring(0, 10) + "...",
    redirect_uri,
    client_id,
  });

  if (grant_type !== "authorization_code") {
    return res.status(400).json({
      error: "unsupported_grant_type",
      error_description: "Only authorization_code grant type is supported",
    });
  }

  const authData = authCodes.get(code);
  if (!authData || authData.expires < Date.now()) {
    return res.status(400).json({
      error: "invalid_grant",
      error_description: "Invalid or expired authorization code",
    });
  }

  // Validate redirect_uri and client_id
  if (
    authData.redirect_uri !== redirect_uri ||
    authData.client_id !== client_id
  ) {
    return res.status(400).json({
      error: "invalid_grant",
      error_description: "Redirect URI or client ID mismatch",
    });
  }

  // Generate access token
  const accessToken = uuidv4();
  const idToken = await generateMockIdToken(authData.user);

  // Store access token
  accessTokens.set(accessToken, {
    user: authData.user,
    expires: Date.now() + 3600000, // 1 hour
  });

  // Clean up auth code
  authCodes.delete(code);

  res.json({
    access_token: accessToken,
    token_type: "Bearer",
    expires_in: 3600,
    id_token: idToken,
    scope: "openid email profile",
  });
});

// OAuth userinfo endpoint
app.get("/oauth2/v2/userinfo", (req, res) => {
  const authHeader = req.headers.authorization;
  if (!authHeader || !authHeader.startsWith("Bearer ")) {
    return res.status(401).json({
      error: "invalid_token",
      error_description: "Invalid or missing access token",
    });
  }

  const accessToken = authHeader.substring(7);
  const tokenData = accessTokens.get(accessToken);

  if (!tokenData || tokenData.expires < Date.now()) {
    return res.status(401).json({
      error: "invalid_token",
      error_description: "Invalid or expired access token",
    });
  }

  res.json(tokenData.user);
});

// Mock user selection endpoint for development
app.get("/dev/select-user", (req, res) => {
  const users = Object.values(mockUsers);
  res.send(`
    <!DOCTYPE html>
    <html>
      <head>
        <title>Mock OAuth - Select User</title>
        <style>
          :root { color-scheme: dark; }
          body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', 'Apple Color Emoji', 'Segoe UI Emoji';
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
          ${users
            .map(
              (user) => `
            <div class="user-card" onclick="selectUser('${user.email}')">
              <h3>${user.name}</h3>
              <p class="email">${user.email}</p>
            </div>
          `
            )
            .join("")}
        </div>
        <script>
          function selectUser(email) {
            const urlParams = new URLSearchParams(window.location.search);
            const params = Object.fromEntries(urlParams);

            // Store selected user in session
            sessionStorage.setItem('selectedUser', email);

            // Redirect to auth endpoint with original params
            const authUrl = new URL('/oauth2/v2/auth', window.location.origin);
            Object.entries(params).forEach(([key, value]) => {
              authUrl.searchParams.set(key, value);
            });
            authUrl.searchParams.set('selected_user', email);

            window.location.href = authUrl.toString();
          }
        </script>
      </body>
    </html>
  `);
});

// Generate mock ID token (simplified JWT-like structure)
function generateMockIdToken(user) {
  const iss = `${process.env.OAUTH_ISSUER || "http://localhost:3001"}`;
  const now = Math.floor(Date.now() / 1000);
  const payload = {
    iss,
    sub: user.id,
    aud: "mock-client-id",
    exp: now + 3600,
    iat: now,
    email: user.email,
    email_verified: user.verified_email,
    name: user.name,
    picture: user.picture,
  };
  const header = { alg: "RS256", kid, typ: "JWT" };
  const jwt = new jose.SignJWT(payload)
    .setProtectedHeader(header)
    .setIssuer(iss)
    .setAudience("mock-client-id")
    .setIssuedAt()
    .setExpirationTime("1h");
  return jwt.sign(privateKey);
}

// Health check endpoint
app.get("/health", (req, res) => {
  res.json({ status: "healthy", timestamp: new Date().toISOString() });
});

// Development info endpoint
app.get("/", (req, res) => {
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
    environment: process.env.NODE_ENV || "development",
  });
});

initKeys().then(() => {
  app.listen(PORT, "0.0.0.0", () => {
    console.log(`Mock OAuth server running on port ${PORT}`);
    console.log(`Visit http://localhost:${PORT} for endpoint info`);
    console.log("Mock users available:", Object.keys(mockUsers).join(", "));
  });
});

module.exports = app;
