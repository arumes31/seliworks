CREATE TABLE user_totp (
 user_id uuid PRIMARY KEY REFERENCES users(id), secret bytea NOT NULL, pending_secret bytea, enabled boolean NOT NULL DEFAULT false, last_counter bigint NOT NULL DEFAULT -1, updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE recovery_codes (user_id uuid NOT NULL REFERENCES users(id), code_hash text NOT NULL, PRIMARY KEY(user_id,code_hash));
