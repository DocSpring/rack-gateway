-- The rack overwrites an object that's uploaded again under the same key, so a build could use an archive other
-- than the one the gateway checked. Object keys are write-once through the gateway: an upload claims its key here
-- before it's forwarded, and an upload to a claimed key is refused, even when the first one failed (the rack may
-- still have stored it). Checking a build's archive claims its key too. uploaded_at is set when the rack answers an
-- upload with its object: a build can't use an archive whose upload hasn't finished. The Convox CLI uploads every
-- archive to a new random key.
CREATE TABLE IF NOT EXISTS rack_object_keys (
  app TEXT NOT NULL,
  object_key TEXT NOT NULL,
  claimed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  uploaded_at TIMESTAMPTZ,
  PRIMARY KEY (app, object_key)
);

-- Archives already bound to a deploy approval (object://<app>/<key>) were protected by that binding until now.
INSERT INTO rack_object_keys (app, object_key, claimed_at, uploaded_at)
SELECT split_part(substring(object_url FROM 10), '/', 1),
       substring(substring(object_url FROM 10) FROM position('/' IN substring(object_url FROM 10)) + 1),
       updated_at,
       object_uploaded_at
FROM deploy_approval_requests
WHERE object_url LIKE 'object://%/%'
ON CONFLICT DO NOTHING;
