-- Enrich audit logs with API token metadata and enforce API token name uniqueness.

-- Trim whitespace from existing token names to avoid false duplicates.
UPDATE api_tokens
SET name = TRIM(name)
WHERE name <> TRIM(name);

-- Deduplicate any remaining duplicate names by suffixing the token id.
WITH ranked AS (
    SELECT id,
           name,
           ROW_NUMBER() OVER (PARTITION BY name ORDER BY id) AS rn,
           CHAR_LENGTH(name) AS name_len,
           CHAR_LENGTH(id::text) AS id_len
    FROM api_tokens
)
UPDATE api_tokens AS t
SET name = CASE
              WHEN ranked.name_len + 2 + ranked.id_len <= 150 THEN t.name || ' #' || t.id
              ELSE LEFT(t.name, GREATEST(150 - ranked.id_len - 2, 0)) || ' #' || t.id
           END
FROM ranked
WHERE t.id = ranked.id
  AND ranked.rn > 1;

-- Enforce unique API token names going forward.
ALTER TABLE api_tokens
    ADD CONSTRAINT api_tokens_name_unique UNIQUE (name);

-- Capture API token metadata on audit log entries.
ALTER TABLE audit_logs
    ADD COLUMN api_token_id BIGINT REFERENCES api_tokens(id) ON DELETE SET NULL,
    ADD COLUMN api_token_name VARCHAR(150);

CREATE INDEX IF NOT EXISTS idx_audit_logs_api_token_id ON audit_logs(api_token_id);
