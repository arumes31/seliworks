-- Zeitausgleich (TIME_OFF) is priced like a regular day; the work note is optional.
SET CONSTRAINTS validate_session_totals IMMEDIATE;
ALTER TABLE work_sessions DROP CONSTRAINT work_sessions_day_type_check;
ALTER TABLE work_sessions ADD CONSTRAINT work_sessions_day_type_check CHECK(day_type IN('REGULAR','VACATION','HOLIDAY','TIME_OFF'));
DO $$ DECLARE c text; BEGIN
 SELECT conname INTO STRICT c FROM pg_constraint WHERE conrelid='work_sessions'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%description%activity_id%';
 EXECUTE format('ALTER TABLE work_sessions DROP CONSTRAINT %I', c);
END $$;
ALTER TABLE work_sessions ADD CONSTRAINT complete_work_activity CHECK(state NOT IN('SUBMITTED','APPROVED') OR activity_id IS NOT NULL);
SET CONSTRAINTS validate_session_totals DEFERRED;
