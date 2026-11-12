ALTER TABLE users DROP COLUMN IF EXISTS pending_email;
DROP TABLE IF EXISTS email_confirmations;
