# Mock OAuth Server - Claude Development Guide

## Overview

A mock Google OAuth 2.0 server for local development and testing. Simulates Google's OAuth flow without requiring real Google credentials.

## Purpose

- **Local development**: No need for real Google OAuth credentials
- **E2E testing**: Fully automated OAuth flows in tests
- **Deterministic**: Consistent behavior for testing

## Tech Stack

- **Runtime**: Node.js with TypeScript
- **Framework**: Express
- **Port**: 3345 (configurable via `MOCK_OAUTH_PORT`)

## Task Commands

| Command                 | Description             |
| ----------------------- | ----------------------- |
| `task mock-oauth:dev`   | Run mock OAuth server   |
| `task mock-oauth:lint`  | Lint mock OAuth code    |
| `task mock-oauth:build` | Build mock OAuth server |

## Features

### User Selection

The mock server provides a simple UI to select which user to authenticate as:

- admin@example.com (Admin role)
- deployer@example.com (Deployer role)
- ops@example.com (Ops role)
- viewer@example.com (Viewer role)
- e2e-edit@example.com (Test user)

### OAuth Endpoints

**Authorization endpoint:**
```
GET /oauth2/v2/auth
```

Parameters:
- `client_id`: OAuth client ID
- `redirect_uri`: Where to redirect after auth
- `response_type`: Should be "code"
- `scope`: OAuth scopes requested
- `state`: CSRF protection state
- `code_challenge`: PKCE challenge (optional)
- `code_challenge_method`: Should be "S256"

**Token endpoint:**
```
POST /oauth2/v2/token
```

Exchange authorization code for access/refresh tokens.

**UserInfo endpoint:**
```
GET /oauth2/v2/userinfo
```

Returns user profile information.

### E2E Mode

When `window.__e2e_test_mode__` is set, the mock server:
- Auto-selects the user based on URL parameters
- Skips the manual user selection UI
- Enables fully automated OAuth flows in Playwright tests

## Development

### Running Locally

```bash
task mock-oauth:dev
# Server starts on http://localhost:3345
```

### Configuration

Environment variables (set in `mise.toml`):
- `MOCK_OAUTH_PORT`: Port to run on (default: 3345)

### Adding Test Users

Edit `src/users.ts` to add new mock users:

```typescript
export const MOCK_USERS = {
  'newuser@example.com': {
    email: 'newuser@example.com',
    name: 'New User',
    picture: 'https://example.com/avatar.jpg',
  },
}
```

## Testing

The mock OAuth server is automatically started by E2E test tasks:
- `task web:e2e`
- `task go:e2e`

Health check endpoint:
```bash
curl http://localhost:3345/health
# Returns: {"status":"ok"}
```

## Important Notes

- **Not for production**: This is a testing/development tool only
- **No real authentication**: All users are mocked
- **No token validation**: Tokens are simple JWTs with no real security
- **Deterministic behavior**: Always returns the same data for the same inputs

## File Structure

```
mock-oauth/
├── src/
│   ├── index.ts       # Main server
│   ├── users.ts       # Mock user database
│   └── routes/        # OAuth endpoint handlers
├── package.json       # Dependencies
└── tsconfig.json      # TypeScript config
```
