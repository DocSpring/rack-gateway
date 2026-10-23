-- Builds whose manifest the gateway checked with the app's env (service_image_patterns). The rack's build reads the
-- app's env when it starts and substitutes it into the manifest, after the gateway's check, so the app's env can't
-- change while one of these builds may still read it. A row is added, under the app's env lock, before the build is
-- forwarded; build_id is set when the rack answers with the build. The row is deleted once the rack reports the
-- build finished, or the build never reached the rack; rows older than an hour no longer hold up env changes.
CREATE TABLE IF NOT EXISTS checked_builds (
  id BIGSERIAL PRIMARY KEY,
  app TEXT NOT NULL,
  build_id TEXT,
  checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS checked_builds_app_idx ON checked_builds (app, checked_at);
