-- Retain original work and financial evidence; deleted intervals no longer reserve time.
ALTER TABLE work_sessions DROP CONSTRAINT work_sessions_state_check;
ALTER TABLE work_sessions ADD CONSTRAINT work_sessions_state_check
 CHECK(state IN ('RUNNING','STOPPED_PENDING_DETAILS','SUBMITTED','REJECTED','APPROVED','DELETED'));
ALTER TABLE work_sessions DROP CONSTRAINT work_sessions_worker_id_tstzrange_excl;
ALTER TABLE work_sessions ADD CONSTRAINT work_sessions_worker_id_tstzrange_excl
 EXCLUDE USING gist (worker_id WITH =, tstzrange(effective_start,effective_stop,'[)') WITH &&)
 WHERE (state <> 'DELETED');
