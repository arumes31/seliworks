ALTER TABLE users ALTER COLUMN language SET DEFAULT 'de';
CREATE TABLE work_pauses (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), session_id uuid NOT NULL REFERENCES work_sessions(id), started_at timestamptz NOT NULL,
 planned_stop timestamptz, resumed_at timestamptz, actor_id uuid NOT NULL REFERENCES users(id),
 CHECK(planned_stop IS NULL OR planned_stop>started_at), CHECK(resumed_at IS NULL OR resumed_at>=started_at),
 EXCLUDE USING gist(session_id WITH =,tstzrange(started_at,coalesce(resumed_at,planned_stop),'[)') WITH &&)
);
CREATE UNIQUE INDEX one_unfinalized_pause ON work_pauses(session_id) WHERE resumed_at IS NULL;
-- Timer pauses are materialized as explicit breaks when Stop commits. During work,
-- their derived intervals are authoritative without a background timer or job.
CREATE VIEW work_break_intervals AS
 SELECT id,session_id,started_at,stopped_at FROM breaks
 UNION ALL
 SELECT p.id,p.session_id,p.started_at,least(coalesce(p.resumed_at,p.planned_stop,sample.now),sample.now) AS stopped_at
 FROM work_pauses p JOIN work_sessions s ON s.id=p.session_id AND s.state='RUNNING'
 CROSS JOIN (SELECT clock_timestamp() AS now) sample;
