const express = require('express');
const cors = require('cors');
const { v4: uuidv4 } = require('uuid');

const app = express();
const PORT = process.env.PORT || 3001;

// Enable CORS for all routes
app.use(cors());
app.use(express.json());
app.use(express.urlencoded({ extended: true }));

// Mock users database
const mockUsers = {
  'admin@company.com': {
    id: '1',
    email: 'admin@company.com',
    name: 'Admin User',
    picture: 'https://via.placeholder.com/128',
    verified_email: true,
  },
  'developer@company.com': {
    id: '2', 
    email: 'developer@company.com',
    name: 'Developer User',
    picture: 'https://via.placeholder.com/128',
    verified_email: true,
  },
  'viewer@company.com': {
    id: '3',
    email: 'viewer@company.com', 
    name: 'Viewer User',
    picture: 'https://via.placeholder.com/128',
    verified_email: true,
  },
};

// Store authorization codes temporarily
const authCodes = new Map();
const accessTokens = new Map();

// Resolve base URL from env for consistent external redirects (browser)
function getBaseUrl(req) {
  return process.env.OAUTH_ISSUER || `${req.protocol}://${req.get('host')}`;
}

// OAuth discovery endpoint
app.get('/.well-known/openid-configuration', (req, res) => {
  const baseUrl = getBaseUrl(req);
  res.json({
    issuer: baseUrl,
    authorization_endpoint: `${baseUrl}/oauth2/v2/auth`,
    token_endpoint: `${baseUrl}/oauth2/v4/token`,
    userinfo_endpoint: `${baseUrl}/oauth2/v2/userinfo`,
    jwks_uri: `${baseUrl}/.well-known/jwks`,
    response_types_supported: ['code'],
    grant_types_supported: ['authorization_code'],
    subject_types_supported: ['public'],
    id_token_signing_alg_values_supported: ['RS256'],
    scopes_supported: ['openid', 'email', 'profile'],
  });
});

// OAuth authorization endpoint with user selection
app.get('/oauth2/v2/auth', (req, res) => {
  const {
    client_id,
    redirect_uri,
    response_type,
    scope,
    state,
    code_challenge,
    code_challenge_method,
    selected_user
  } = req.query;

  console.log('OAuth authorization request:', {
    client_id,
    redirect_uri,
    response_type,
    scope,
    state,
    selected_user
  });

  // If no user selected, show user selection page
  if (!selected_user) {
    return res.redirect(`/dev/select-user?${req.url.split('?')[1]}`);
  }

  // Validate required parameters
  if (!client_id || !redirect_uri || response_type !== 'code') {
    return res.status(400).json({
      error: 'invalid_request',
      error_description: 'Missing or invalid required parameters'
    });
  }

  const user = mockUsers[selected_user];
  if (!user) {
    return res.status(400).json({
      error: 'invalid_user',
      error_description: 'Selected user not found'
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
  redirectUrl.searchParams.set('code', authCode);
  if (state) redirectUrl.searchParams.set('state', state);

  res.redirect(redirectUrl.toString());
});

// OAuth token endpoint
app.post('/oauth2/v4/token', (req, res) => {
  const {
    grant_type,
    code,
    redirect_uri,
    client_id,
    code_verifier
  } = req.body;

  console.log('OAuth token request:', {
    grant_type,
    code: code?.substring(0, 10) + '...',
    redirect_uri,
    client_id
  });

  if (grant_type !== 'authorization_code') {
    return res.status(400).json({
      error: 'unsupported_grant_type',
      error_description: 'Only authorization_code grant type is supported'
    });
  }

  const authData = authCodes.get(code);
  if (!authData || authData.expires < Date.now()) {
    return res.status(400).json({
      error: 'invalid_grant',
      error_description: 'Invalid or expired authorization code'
    });
  }

  // Validate redirect_uri and client_id
  if (authData.redirect_uri !== redirect_uri || authData.client_id !== client_id) {
    return res.status(400).json({
      error: 'invalid_grant',
      error_description: 'Redirect URI or client ID mismatch'
    });
  }

  // Generate access token
  const accessToken = uuidv4();
  const idToken = generateMockIdToken(authData.user);
  
  // Store access token
  accessTokens.set(accessToken, {
    user: authData.user,
    expires: Date.now() + 3600000, // 1 hour
  });

  // Clean up auth code
  authCodes.delete(code);

  res.json({
    access_token: accessToken,
    token_type: 'Bearer',
    expires_in: 3600,
    id_token: idToken,
    scope: 'openid email profile',
  });
});

// OAuth userinfo endpoint
app.get('/oauth2/v2/userinfo', (req, res) => {
  const authHeader = req.headers.authorization;
  if (!authHeader || !authHeader.startsWith('Bearer ')) {
    return res.status(401).json({
      error: 'invalid_token',
      error_description: 'Invalid or missing access token'
    });
  }

  const accessToken = authHeader.substring(7);
  const tokenData = accessTokens.get(accessToken);
  
  if (!tokenData || tokenData.expires < Date.now()) {
    return res.status(401).json({
      error: 'invalid_token',
      error_description: 'Invalid or expired access token'
    });
  }

  res.json(tokenData.user);
});

// Mock user selection endpoint for development
app.get('/dev/select-user', (req, res) => {
  const users = Object.values(mockUsers);
  res.send(`
    <!DOCTYPE html>
    <html>
      <head>
        <title>Mock OAuth - Select User</title>
        <style>
          body { font-family: Arial, sans-serif; margin: 40px; }
          .user-card { 
            border: 1px solid #ddd; 
            padding: 20px; 
            margin: 10px 0; 
            border-radius: 5px;
            cursor: pointer;
          }
          .user-card:hover { background-color: #f5f5f5; }
        </style>
      </head>
      <body>
        <h1>Mock Google OAuth - Select User</h1>
        <p>Choose which user to authenticate as:</p>
        ${users.map(user => `
          <div class="user-card" onclick="selectUser('${user.email}')">
            <h3>${user.name}</h3>
            <p>${user.email}</p>
          </div>
        `).join('')}
        
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
  const header = Buffer.from(JSON.stringify({ alg: 'RS256', typ: 'JWT' })).toString('base64url');
  const payload = Buffer.from(JSON.stringify({
    iss: `${process.env.OAUTH_ISSUER || 'http://localhost:3001'}`,
    sub: user.id,
    aud: 'mock-client-id',
    exp: Math.floor(Date.now() / 1000) + 3600,
    iat: Math.floor(Date.now() / 1000),
    email: user.email,
    email_verified: user.verified_email,
    name: user.name,
    picture: user.picture,
  })).toString('base64url');
  const signature = 'mock-signature'; // In real implementation, this would be cryptographically signed
  
  return `${header}.${payload}.${signature}`;
}

// Health check endpoint
app.get('/health', (req, res) => {
  res.json({ status: 'healthy', timestamp: new Date().toISOString() });
});

// Development info endpoint
app.get('/', (req, res) => {
  res.json({
    name: 'Mock Google OAuth Server',
    version: '1.0.0',
    endpoints: {
      discovery: '/.well-known/openid_configuration',
      authorization: '/oauth2/v2/auth',
      token: '/oauth2/v4/token',
      userinfo: '/oauth2/v2/userinfo',
      dev_user_selector: '/dev/select-user',
      health: '/health'
    },
    mock_users: Object.keys(mockUsers),
    environment: process.env.NODE_ENV || 'development'
  });
});

app.listen(PORT, '0.0.0.0', () => {
  console.log(`Mock OAuth server running on port ${PORT}`);
  console.log(`Visit http://localhost:${PORT} for endpoint info`);
  console.log('Mock users available:', Object.keys(mockUsers).join(', '));
});

module.exports = app;
