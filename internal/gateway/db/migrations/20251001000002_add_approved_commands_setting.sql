-- Add approved_commands setting with empty default (must be configured via UI or env var)
INSERT INTO settings (key, value, updated_at)
VALUES (
  'approved_commands',
  '{"commands": []}'::jsonb,
  NOW()
)
ON CONFLICT (key) DO NOTHING;
