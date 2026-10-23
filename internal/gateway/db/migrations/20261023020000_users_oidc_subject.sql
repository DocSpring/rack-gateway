-- The identity provider's subject (the ID token's sub claim) of the account that signs in as each user. It's stored
-- at the user's first sign-in, and a later sign-in with the same email but another subject is refused, so an email
-- address that's reassigned to someone else doesn't inherit the user. Changing the user's email clears it, and so
-- does an admin's reset (POST /api/v1/users/:email/sign-in/reset). Existing users bind at their next sign-in.
ALTER TABLE users ADD COLUMN oidc_subject TEXT UNIQUE;
