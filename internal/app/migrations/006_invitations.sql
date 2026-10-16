CREATE TABLE invitations (
 token_hash text PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id), created_by uuid NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL, expires_at timestamptz NOT NULL, used_at timestamptz,
 CHECK(expires_at>created_at)
);
CREATE INDEX invitation_user ON invitations(user_id);
