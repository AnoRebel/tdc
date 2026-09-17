ALTER TABLE users DROP COLUMN IF EXISTS webauthn_handle;
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_passkey_shape;
ALTER TABLE credentials DROP COLUMN IF EXISTS passkey;
