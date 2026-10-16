ALTER TABLE work_sessions
 ADD COLUMN recorded_worker_id uuid REFERENCES users(id),
 ADD COLUMN recorded_farm_id uuid REFERENCES farms(id),
 ADD COLUMN is_vacation_day boolean,
 ADD COLUMN additional_deduction_us bigint NOT NULL DEFAULT 0 CHECK(additional_deduction_us>=0),
 ADD COLUMN deduction_reason text NOT NULL DEFAULT '' CHECK(length(deduction_reason)<=2000);
UPDATE work_sessions SET recorded_worker_id=worker_id,recorded_farm_id=farm_id,is_vacation_day=CASE WHEN state='RUNNING' THEN NULL ELSE false END;
ALTER TABLE work_sessions ALTER COLUMN recorded_worker_id SET NOT NULL,ALTER COLUMN recorded_farm_id SET NOT NULL;
ALTER TABLE work_sessions ADD CONSTRAINT complete_stop_answers CHECK(state NOT IN ('SUBMITTED','APPROVED') OR is_vacation_day IS NOT NULL);
ALTER TABLE work_sessions ALTER COLUMN is_vacation_day SET DEFAULT false;
ALTER TABLE work_sessions ADD CONSTRAINT explained_deduction CHECK(additional_deduction_us=0 OR length(trim(deduction_reason))>0);
CREATE FUNCTION record_original_assignment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 NEW.recorded_worker_id=NEW.worker_id;NEW.recorded_farm_id=NEW.farm_id;RETURN NEW;
END $$;
CREATE TRIGGER original_assignment BEFORE INSERT ON work_sessions FOR EACH ROW EXECUTE FUNCTION record_original_assignment();
CREATE OR REPLACE FUNCTION immutable_evidence() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.started_at IS DISTINCT FROM OLD.started_at OR (OLD.stopped_at IS NOT NULL AND NEW.stopped_at IS DISTINCT FROM OLD.stopped_at) OR NEW.recorded_worker_id<>OLD.recorded_worker_id OR NEW.recorded_farm_id<>OLD.recorded_farm_id THEN
 RAISE EXCEPTION 'Original recording is immutable' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE FUNCTION validate_work_totals() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE sid uuid;s work_sessions;net_before_deduction bigint;
BEGIN
 IF TG_TABLE_NAME='work_sessions' THEN sid=NEW.id;ELSIF TG_OP='DELETE' THEN sid=OLD.session_id;ELSE sid=NEW.session_id;END IF;
 SELECT * INTO s FROM work_sessions WHERE id=sid;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF s.effective_stop IS NULL THEN
  IF s.additional_deduction_us<>0 THEN RAISE EXCEPTION 'Additional deduction requires closed work' USING ERRCODE='23514';END IF;
  RETURN NULL;
 END IF;
 IF EXISTS(SELECT 1 FROM breaks WHERE session_id=sid AND (started_at<s.effective_start OR stopped_at>s.effective_stop)) THEN
 RAISE EXCEPTION 'Break outside effective work interval' USING ERRCODE='23514';END IF;
 SELECT (extract(epoch FROM(s.effective_stop-s.effective_start))*1000000)::bigint-coalesce(sum((extract(epoch FROM(b.stopped_at-b.started_at))*1000000)::bigint),0) INTO net_before_deduction FROM breaks b WHERE session_id=sid;
 IF s.additional_deduction_us>net_before_deduction THEN RAISE EXCEPTION 'Additional deduction exceeds remaining work' USING ERRCODE='23514';END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER validate_session_totals AFTER INSERT OR UPDATE ON work_sessions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_work_totals();
CREATE CONSTRAINT TRIGGER validate_break_totals AFTER INSERT OR UPDATE OR DELETE ON breaks DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_work_totals();
