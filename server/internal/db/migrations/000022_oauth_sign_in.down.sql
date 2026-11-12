ALTER TABLE credentials DROP COLUMN IF EXISTS external_email_verified;
ALTER TABLE credentials DROP COLUMN IF EXISTS external_email;
DROP TABLE IF EXISTS oauth_authorizations;
