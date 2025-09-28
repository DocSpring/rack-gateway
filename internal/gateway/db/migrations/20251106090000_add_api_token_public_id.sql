CREATE EXTENSION IF NOT EXISTS "pgcrypto";

ALTER TABLE api_tokens
    ADD COLUMN public_id UUID;

UPDATE api_tokens
   SET public_id = gen_random_uuid()
 WHERE public_id IS NULL;

ALTER TABLE api_tokens
    ALTER COLUMN public_id SET DEFAULT gen_random_uuid(),
    ALTER COLUMN public_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_public_id ON api_tokens(public_id);
