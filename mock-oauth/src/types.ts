export interface MockUser {
  id: string;
  email: string;
  name: string;
  picture: string;
  verified_email: boolean;
}

export interface AuthorizationCodePayload {
  clientId: string;
  redirectUri: string;
  codeChallenge?: string;
  codeChallengeMethod?: string;
  user: MockUser;
  expires: number;
}

export interface AccessTokenPayload {
  user: MockUser;
  expires: number;
}

export interface TokenRequestBody {
  grant_type?: string;
  code?: string;
  redirect_uri?: string;
  code_verifier?: string;
  client_id?: string;
}
