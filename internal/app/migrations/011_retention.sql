ALTER TABLE organization
 ADD COLUMN security_retention_days integer NOT NULL DEFAULT 30 CHECK(security_retention_days BETWEEN 1 AND 3650),
 ADD COLUMN retention_notes text NOT NULL DEFAULT '' CHECK(length(retention_notes)<=2000);
CREATE INDEX auth_session_expiry ON auth_sessions(expires_at);
CREATE INDEX invitation_expiry ON invitations(expires_at);
CREATE INDEX auth_limit_age ON auth_limits(window_start);
