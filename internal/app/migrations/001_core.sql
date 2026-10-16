CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE TABLE organization (
 id boolean PRIMARY KEY DEFAULT true CHECK(id), name text NOT NULL DEFAULT 'SeliWorks', timezone text NOT NULL DEFAULT 'Europe/Vienna', currency text NOT NULL DEFAULT 'EUR' CHECK(currency ~ '^[A-Z]{3}$'),
 address text NOT NULL DEFAULT '', email text NOT NULL DEFAULT '', bank_details text NOT NULL DEFAULT '', tax_percent numeric(7,4) NOT NULL DEFAULT 0 CHECK(tax_percent BETWEEN 0 AND 100), tax_confirmed boolean NOT NULL DEFAULT false,
 invoice_prefix text NOT NULL DEFAULT 'SW', payment_days integer NOT NULL DEFAULT 14 CHECK(payment_days BETWEEN 0 AND 365), long_session_hours integer NOT NULL DEFAULT 14 CHECK(long_session_hours BETWEEN 1 AND 744), version bigint NOT NULL DEFAULT 1
);
INSERT INTO organization(id) VALUES(true);
CREATE TABLE users (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), email text NOT NULL UNIQUE CHECK(email=lower(email)), name text NOT NULL, password_hash text NOT NULL,
 is_admin boolean NOT NULL DEFAULT false, is_worker boolean NOT NULL DEFAULT true, active boolean NOT NULL DEFAULT true, language text NOT NULL DEFAULT 'en' CHECK(language IN ('en','de')), theme text NOT NULL DEFAULT 'system' CHECK(theme IN ('system','light','dark')),
 version bigint NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT clock_timestamp(), CHECK(is_admin OR is_worker)
);
CREATE TABLE auth_sessions (
 token_hash text PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id), csrf_token text NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(), expires_at timestamptz NOT NULL
);
CREATE INDEX auth_user ON auth_sessions(user_id);
CREATE TABLE auth_limits (bucket text PRIMARY KEY, window_start timestamptz NOT NULL, attempts integer NOT NULL);
CREATE TABLE farms (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, address text NOT NULL DEFAULT '', email text NOT NULL DEFAULT '', active boolean NOT NULL DEFAULT true, version bigint NOT NULL DEFAULT 1);
CREATE TABLE worker_farms (worker_id uuid NOT NULL REFERENCES users(id), farm_id uuid NOT NULL REFERENCES farms(id), PRIMARY KEY(worker_id,farm_id));
CREATE TABLE activities (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, active boolean NOT NULL DEFAULT true, version bigint NOT NULL DEFAULT 1);
INSERT INTO activities(name) VALUES ('Feeding animals'),('Stable work'),('Field work'),('Harvesting'),('Machinery maintenance'),('Transport'),('Other');
CREATE TABLE work_sessions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), worker_id uuid NOT NULL REFERENCES users(id), farm_id uuid NOT NULL REFERENCES farms(id), activity_id uuid REFERENCES activities(id),
 started_at timestamptz NOT NULL, stopped_at timestamptz, effective_start timestamptz NOT NULL, effective_stop timestamptz,
 state text NOT NULL DEFAULT 'RUNNING' CHECK(state IN ('RUNNING','STOPPED_PENDING_DETAILS','SUBMITTED','REJECTED','APPROVED')),
 description text NOT NULL DEFAULT '' CHECK(length(description)<=4000), location text NOT NULL DEFAULT '' CHECK(length(location)<=300), non_billable boolean NOT NULL DEFAULT false,
 version bigint NOT NULL DEFAULT 1, created_by uuid NOT NULL REFERENCES users(id), updated_at timestamptz NOT NULL,
 CHECK(stopped_at IS NULL OR stopped_at>=started_at), CHECK(effective_stop IS NULL OR effective_stop>=effective_start),
 CHECK((state='RUNNING' AND stopped_at IS NULL AND effective_stop IS NULL) OR (state<>'RUNNING' AND stopped_at IS NOT NULL AND effective_stop IS NOT NULL)),
 CHECK(state NOT IN ('SUBMITTED','APPROVED') OR (length(trim(description))>0 AND activity_id IS NOT NULL)),
 EXCLUDE USING gist (worker_id WITH =, tstzrange(effective_start,effective_stop,'[)') WITH &&)
);
CREATE UNIQUE INDEX one_open_work_session_per_worker ON work_sessions(worker_id) WHERE stopped_at IS NULL;
CREATE INDEX work_date ON work_sessions(effective_start DESC);
CREATE INDEX work_worker ON work_sessions(worker_id,effective_start DESC);
CREATE INDEX work_farm_state ON work_sessions(farm_id,state,effective_start);
CREATE TABLE breaks (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), session_id uuid NOT NULL REFERENCES work_sessions(id), started_at timestamptz NOT NULL, stopped_at timestamptz NOT NULL CHECK(stopped_at>started_at),
 EXCLUDE USING gist (session_id WITH =, tstzrange(started_at,stopped_at,'[)') WITH &&)
);
CREATE FUNCTION check_break_bounds() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s work_sessions;
BEGIN
 SELECT * INTO s FROM work_sessions WHERE id=NEW.session_id FOR UPDATE;
 IF s.effective_stop IS NULL OR NEW.started_at<s.effective_start OR NEW.stopped_at>s.effective_stop THEN RAISE EXCEPTION 'Break outside closed session' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER breaks_within_session BEFORE INSERT OR UPDATE ON breaks FOR EACH ROW EXECUTE FUNCTION check_break_bounds();
CREATE TABLE corrections (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), session_id uuid NOT NULL REFERENCES work_sessions(id), requested_by uuid NOT NULL REFERENCES users(id), source_version bigint NOT NULL,
 requested_start timestamptz NOT NULL, requested_stop timestamptz NOT NULL CHECK(requested_stop>requested_start), reason text NOT NULL CHECK(length(trim(reason))>0),
 state text NOT NULL DEFAULT 'PENDING' CHECK(state IN ('PENDING','APPROVED','REJECTED')), reviewed_by uuid REFERENCES users(id), review_reason text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT clock_timestamp(), reviewed_at timestamptz
);
CREATE TABLE approvals (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), session_id uuid NOT NULL REFERENCES work_sessions(id), source_version bigint NOT NULL, actor_id uuid NOT NULL REFERENCES users(id), created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 snapshot jsonb NOT NULL, UNIQUE(session_id,source_version)
);
CREATE TABLE commands (
 actor_id uuid NOT NULL REFERENCES users(id), operation text NOT NULL, key text NOT NULL CHECK(length(key) BETWEEN 8 AND 200), fingerprint text NOT NULL,
 result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(actor_id,operation,key)
);
CREATE TABLE audit_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, actor_id uuid REFERENCES users(id), action text NOT NULL, entity_id text NOT NULL, before_value jsonb, after_value jsonb, created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX audit_recent ON audit_events(created_at DESC);
CREATE FUNCTION immutable_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.started_at IS DISTINCT FROM OLD.started_at OR (OLD.stopped_at IS NOT NULL AND NEW.stopped_at IS DISTINCT FROM OLD.stopped_at) OR NEW.worker_id<>OLD.worker_id OR NEW.farm_id<>OLD.farm_id THEN
 RAISE EXCEPTION 'Original recording is immutable' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER original_recording BEFORE UPDATE ON work_sessions FOR EACH ROW EXECUTE FUNCTION immutable_evidence();
