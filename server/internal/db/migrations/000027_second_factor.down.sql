ALTER TABLE credentials DROP COLUMN IF EXISTS confirmed_at;
ALTER TABLE credentials DROP COLUMN IF EXISTS last_period;
ALTER TABLE credentials DROP COLUMN IF EXISTS used_at;
DROP INDEX IF EXISTS credentials_one_totp_per_user_idx;

-- Rows of the new kinds go before the constraint that permitted them, or the
-- constraint cannot be restored.
DELETE FROM credentials WHERE kind IN ('totp', 'recovery');

ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_kind_valid;
ALTER TABLE credentials
  ADD CONSTRAINT credentials_kind_valid
    CHECK (kind IN ('password', 'passkey', 'oauth2'));
