-- Locking a user (locked_at) is how an account is disabled. Nothing has read or written suspended since v0.1.11,
-- which shipped with the column still in place so v0.1.10 could keep running during that rollout.
ALTER TABLE users DROP COLUMN IF EXISTS suspended;
