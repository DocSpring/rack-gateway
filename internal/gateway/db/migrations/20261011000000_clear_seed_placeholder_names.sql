-- Users seeded from ADMIN_USERS etc. were named "Admin User" (and the seed reset the name on every start), so
-- emails and the audit log showed the placeholder. Seeded users now have no name until they sign in, when it's
-- filled from the identity provider. Clear the old placeholders so that happens for existing users too.
UPDATE users
  SET name = '', updated_at = NOW()
  WHERE name IN ('Admin User', 'Viewer User', 'Deployer User', 'Ops User');
