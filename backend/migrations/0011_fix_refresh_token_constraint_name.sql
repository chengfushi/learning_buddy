-- Align the pre-existing 0010 unique constraint with GORM's stable constraint name.
DO $migration$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'refresh_tokens'::regclass
      AND conname = 'refresh_tokens_token_hash_key'
  ) AND NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'refresh_tokens'::regclass
      AND conname = 'uni_refresh_tokens_token_hash'
  ) THEN
    ALTER TABLE refresh_tokens
      RENAME CONSTRAINT refresh_tokens_token_hash_key TO uni_refresh_tokens_token_hash;
  ELSIF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'refresh_tokens'::regclass
      AND conname = 'uni_refresh_tokens_token_hash'
  ) THEN
    ALTER TABLE refresh_tokens
      ADD CONSTRAINT uni_refresh_tokens_token_hash UNIQUE (token_hash);
  END IF;
END
$migration$;
