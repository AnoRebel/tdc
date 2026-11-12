ALTER TABLE pending_sign_ins
  DROP COLUMN IF EXISTS emailed_code_expires_at,
  DROP COLUMN IF EXISTS emailed_code_hash;
