-- exec_commands maps each process to the list of commands its execs ran, in order. It used to hold one command per
-- process, and a second exec on the same process replaced the first. Each command stored that way becomes a list of
-- one; the append also turns a value the previous release writes (between this migration and the promote) into a
-- list.
UPDATE deploy_approval_requests
SET exec_commands = (
  SELECT jsonb_object_agg(
    key,
    CASE jsonb_typeof(value) WHEN 'array' THEN value ELSE jsonb_build_array(value) END
  )
  FROM jsonb_each(exec_commands)
)
WHERE exec_commands IS NOT NULL AND exec_commands <> '{}'::jsonb;
