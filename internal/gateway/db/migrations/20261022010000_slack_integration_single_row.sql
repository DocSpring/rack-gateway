-- A gateway has one Slack integration. Reconnecting replaces it in one transaction (ReplaceSlackIntegration), and
-- this index makes a second row impossible. A database that ended up with several (two reconnects at once, before
-- the replacement was atomic) keeps the newest.
DELETE FROM slack_integration WHERE id <> (SELECT max(id) FROM slack_integration);
CREATE UNIQUE INDEX IF NOT EXISTS slack_integration_single_row ON slack_integration ((true));
