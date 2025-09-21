# Mock Google OAuth Server

A simple Node.js server that mimics Google's OAuth 2.0 endpoints for development and testing.

## Features

- **OAuth 2.0 Authorization Code Flow** - Implements the standard OAuth flow
- **PKCE Support** - Code Challenge/Verifier for security
- **Multiple Test Users** - Pre-configured test users with different roles
- **User Selection UI** - Web interface to choose which user to authenticate as
- **OpenID Connect** - Basic OIDC compliance with discovery endpoint
- **Token Management** - Issues access tokens and ID tokens

## Test Users

The dev server comes with four pre-configured test users:

- `admin@example.com` - Admin User
- `deployer@example.com` - Deployer User
- `ops@example.com` - Ops User
- `viewer@example.com` - Viewer User

## Endpoints

- `/.well-known/openid_configuration` - OIDC discovery
- `/oauth2/v2/auth` - Authorization endpoint
- `/oauth2/v4/token` - Token endpoint
- `/oauth2/v2/userinfo` - User info endpoint
- `/dev/select-user` - Development user selection page
- `/health` - Health check
- `/` - Server info and endpoint list

## Environment Variables

- `PORT` - Server port (default: 3345 via `MOCK_OAUTH_PORT`)
- `NODE_ENV` - Environment (default: development)
- `OAUTH_ISSUER` - OAuth issuer URL (default: http://localhost:3345)

## Usage

### Standalone

```bash
cd dev/mock-oauth
npm install
npm start
```

### With Docker

```bash
docker build -t mock-oauth ./dev/mock-oauth
docker run -p 3345:3345 mock-oauth
```

### With Docker Compose

The mock OAuth server is included in the main development setup:

```bash
# From project root
task dev
```

## Integration

Configure your OAuth client to use these endpoints:

```javascript
const config = {
  clientId: "mock-client-id",
  clientSecret: "mock-client-secret",
  authorizeURL: "http://localhost:3345/oauth2/v2/auth",
  tokenURL: "http://localhost:3345/oauth2/v4/token",
  userInfoURL: "http://localhost:3345/oauth2/v2/userinfo",
};
```

## Security Note

⚠️ **This is for development only!** The mock server:

- Uses predictable tokens
- Has no real cryptographic signing
- Stores data in memory
- Has no rate limiting
- Accepts any client credentials

Never use this in production environments.
