ALTER TABLE work_sessions ADD COLUMN additional_activities text NOT NULL DEFAULT '' CHECK(length(additional_activities)<=1000);
CREATE TABLE work_session_activities (
 session_id uuid NOT NULL REFERENCES work_sessions(id) ON DELETE CASCADE,
 activity_id uuid NOT NULL REFERENCES activities(id),
 PRIMARY KEY(session_id,activity_id)
);
