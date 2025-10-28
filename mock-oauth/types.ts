export type MockUser = {
  id: string;
  email: string;
  name: string;
  picture: string;
  verified_email: boolean;
};

export type AuthorizationCodePayload = {
  clientId: string;
  redirectUri: string;
  codeChallenge?: string;
  codeChallengeMethod?: string;
  user: MockUser;
  expires: number;
};

export type AccessTokenPayload = {
  user: MockUser;
  expires: number;
};

export type TokenRequestBody = {
  grant_type?: string;
  code?: string;
  redirect_uri?: string;
  code_verifier?: string;
  client_id?: string;
};
