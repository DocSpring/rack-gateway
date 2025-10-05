-- Simplify user lock logic by removing unlock history tracking
-- Just clear locked_at when unlocking instead of tracking unlock timestamps

ALTER TABLE users
    DROP COLUMN IF EXISTS unlocked_at,
    DROP COLUMN IF EXISTS unlocked_by_user_id;
