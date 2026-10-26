-- Each approved_deploy_commands entry is an object now ({"command": ..., "env": [...], "all_env_vars": ...,
-- "write": ...}), so it can say which env vars its command gets. Each command stored as a string becomes an object
-- with just its command, which gets the env vars any command in the app gets. Entries that are already objects stay
-- as they are, so the update can run again.
UPDATE settings
SET value = (
  SELECT COALESCE(
    jsonb_agg(
      CASE jsonb_typeof(entry) WHEN 'string' THEN jsonb_build_object('command', entry) ELSE entry END
      ORDER BY position
    ),
    '[]'::jsonb
  )
  FROM jsonb_array_elements(value) WITH ORDINALITY AS entries(entry, position)
)
WHERE key = 'approved_deploy_commands' AND jsonb_typeof(value) = 'array';
