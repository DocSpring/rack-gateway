-- Internal metadata for environment tracking
CREATE TABLE IF NOT EXISTS rgw_internal_metadata (
  id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
  environment VARCHAR(32) NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (char_length(environment) <= 32)
);
