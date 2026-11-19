-- Sliding 30-day sessions bound to a coarse device class.
-- Existing sessions keep their expiry and adopt the device on their next use.
ALTER TABLE auth_sessions
 ADD COLUMN device text NOT NULL DEFAULT '' CHECK(length(device)<=64),
 ADD COLUMN renewed_at timestamptz NOT NULL DEFAULT clock_timestamp();
