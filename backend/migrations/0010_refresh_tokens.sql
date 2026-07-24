-- Refresh Token rotation: opaque credentials, hash-only persistence, family revocation.
CREATE TABLE IF NOT EXISTS refresh_tokens (
  id               UUID PRIMARY KEY,
  family_id        UUID NOT NULL,
  user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash       CHAR(64) NOT NULL UNIQUE,
  expires_at       TIMESTAMPTZ NOT NULL,
  used_at          TIMESTAMPTZ,
  revoked_at       TIMESTAMPTZ,
  replaced_by_hash CHAR(64),
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_family ON refresh_tokens(family_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id, created_at DESC);
