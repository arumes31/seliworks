CREATE TABLE passkeys (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id uuid NOT NULL REFERENCES users(id),
 credential_id bytea NOT NULL UNIQUE,
 credential jsonb NOT NULL,
 name text NOT NULL CHECK(length(trim(name)) BETWEEN 1 AND 100),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 last_used_at timestamptz
);
CREATE INDEX passkeys_user ON passkeys(user_id);
CREATE TABLE passkey_ceremonies (
 token_hash text PRIMARY KEY,
 kind text NOT NULL CHECK(kind IN ('registration','login')),
 auth_hash text REFERENCES auth_sessions(token_hash) ON DELETE CASCADE,
 user_id uuid REFERENCES users(id),
 name text NOT NULL DEFAULT '',
 data jsonb NOT NULL,
 expires_at timestamptz NOT NULL,
 CHECK((kind='registration' AND auth_hash IS NOT NULL AND user_id IS NOT NULL) OR (kind='login' AND auth_hash IS NULL AND user_id IS NULL))
);
CREATE INDEX passkey_ceremonies_expiry ON passkey_ceremonies(expires_at);
