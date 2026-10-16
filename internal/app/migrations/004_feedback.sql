ALTER TABLE work_sessions ADD COLUMN rejection_reason text NOT NULL DEFAULT '' CHECK(length(rejection_reason)<=2000);
