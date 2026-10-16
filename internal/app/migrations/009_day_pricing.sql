ALTER TABLE work_sessions ADD COLUMN day_type text CHECK(day_type IN('REGULAR','VACATION','HOLIDAY'));
UPDATE work_sessions SET day_type=CASE WHEN is_vacation_day IS NULL THEN NULL WHEN is_vacation_day THEN 'VACATION' ELSE 'REGULAR' END;
-- Backfilling existing rows queues the deferred duration checks. Drain them
-- before the next ALTER TABLE while retaining deferred checks for later work.
SET CONSTRAINTS validate_session_totals IMMEDIATE;
ALTER TABLE work_sessions ADD CONSTRAINT complete_work_day_type CHECK(state NOT IN('SUBMITTED','APPROVED') OR day_type IS NOT NULL);
SET CONSTRAINTS validate_session_totals DEFERRED;
-- Keep legacy boolean clients compatible while the explicit classification is
-- authoritative. Holiday work is distinct from vacation work.
CREATE FUNCTION normalize_work_day_type() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='UPDATE' AND NEW.day_type IS NOT DISTINCT FROM OLD.day_type AND NEW.is_vacation_day IS DISTINCT FROM OLD.is_vacation_day THEN
  NEW.day_type=CASE WHEN NEW.is_vacation_day IS NULL THEN NULL WHEN NEW.is_vacation_day THEN 'VACATION' ELSE 'REGULAR' END;
 ELSIF NEW.day_type IS NULL AND NEW.is_vacation_day IS NOT NULL THEN
  NEW.day_type=CASE WHEN NEW.is_vacation_day THEN 'VACATION' ELSE 'REGULAR' END;
 END IF;
 IF NEW.day_type IS NULL THEN NEW.is_vacation_day=NULL;ELSE NEW.is_vacation_day=(NEW.day_type='VACATION');END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER normalize_work_day BEFORE INSERT OR UPDATE ON work_sessions FOR EACH ROW EXECUTE FUNCTION normalize_work_day_type();
CREATE TABLE special_day_pricing (
 day_type text PRIMARY KEY CHECK(day_type IN('VACATION','HOLIDAY')),
 mode text CHECK(mode IN('percentage','hourly')),
 value numeric(14,4) CHECK(value>=0),
 version bigint NOT NULL DEFAULT 1,
 CHECK((mode IS NULL AND value IS NULL) OR(mode IS NOT NULL AND value IS NOT NULL))
);
INSERT INTO special_day_pricing(day_type) VALUES('VACATION'),('HOLIDAY');
