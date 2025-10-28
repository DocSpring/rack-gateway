import { exportJWK, generateKeyPair, type JWK, SignJWT } from "jose";
import { v4 as uuidv4 } from "uuid";

import { DEFAULT_PORT, TOPICS } from "./constants.js";
import { logger } from "./logger.js";
import type { MockUser } from "./types.js";

export type PublicJwk = JWK & { kid: string };

export type SigningContext = {
  publicJwk: PublicJwk;
  signingKey: CryptoKey;
  kid: string;
};

let publicJwk: PublicJwk | undefined;
let currentKid: string | undefined;
let signingKey: CryptoKey | undefined;
let keysPromise: Promise<void> | null = null;

export const ensureKeys = (): Promise<void> => {
  if (keysPromise) {
    return keysPromise;
  }
  keysPromise = (async () => {
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
    logger.debug(TOPICS.tokens, `generated signing keys kid=${kid}`);
  })();
  return keysPromise;
};

export const getSigningContext = (): SigningContext => {
  if (!(publicJwk && signingKey && currentKid)) {
    throw new Error("Signing keys have not been initialised");
  }
  return {
    publicJwk,
    signingKey,
    kid: currentKid,
  };
};

export const generateMockIdToken = (user: MockUser): Promise<string> => {
  const { signingKey: signingKeyContext, kid } = getSigningContext();
  const issuer = process.env.OAUTH_ISSUER ?? `http://localhost:${DEFAULT_PORT}`;
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
