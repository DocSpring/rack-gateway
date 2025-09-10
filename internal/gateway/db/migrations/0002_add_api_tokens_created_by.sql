-- Add created_by_user_id to api_tokens and backfill with owner user_id
ALTER TABLE api_tokens ADD COLUMN IF NOT EXISTS created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;

UPDATE api_tokens SET created_by_user_id = user_id WHERE created_by_user_id IS NULL;

