CREATE TABLE rates (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), worker_id uuid REFERENCES users(id), farm_id uuid REFERENCES farms(id), activity_id uuid REFERENCES activities(id),
 hourly_rate numeric(14,4) NOT NULL CHECK(hourly_rate>=0), effective_from timestamptz NOT NULL, effective_to timestamptz, version bigint NOT NULL DEFAULT 1,
 CHECK(effective_to IS NULL OR effective_to>effective_from),
 CHECK((activity_id IS NULL) OR (worker_id IS NOT NULL AND farm_id IS NOT NULL)),
 EXCLUDE USING gist ((coalesce(worker_id,'00000000-0000-0000-0000-000000000000'::uuid)) WITH =, (coalesce(farm_id,'00000000-0000-0000-0000-000000000000'::uuid)) WITH =, (coalesce(activity_id,'00000000-0000-0000-0000-000000000000'::uuid)) WITH =, tstzrange(effective_from,effective_to,'[)') WITH &&)
);
CREATE TABLE invoice_series (prefix text NOT NULL, year integer NOT NULL, next_number bigint NOT NULL CHECK(next_number>0), PRIMARY KEY(prefix,year));
CREATE TABLE invoices (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), farm_id uuid NOT NULL REFERENCES farms(id), state text NOT NULL DEFAULT 'draft' CHECK(state IN ('draft','issued','reversed','cancelled')),
 number text UNIQUE, series text, year integer, serial bigint, from_date date NOT NULL, to_date date NOT NULL CHECK(to_date>from_date),
 period_start timestamptz NOT NULL, period_stop timestamptz NOT NULL, timezone text NOT NULL, currency text NOT NULL,
 issuer jsonb NOT NULL, customer jsonb NOT NULL, net_cents bigint NOT NULL CHECK(net_cents>=0), tax_cents bigint NOT NULL CHECK(tax_cents>=0), total_cents bigint NOT NULL CHECK(total_cents=net_cents+tax_cents),
 created_by uuid NOT NULL REFERENCES users(id), created_at timestamptz NOT NULL DEFAULT clock_timestamp(), issued_at timestamptz, due_on date, reversed_at timestamptz, reversal_reason text, pdf_path text,
 UNIQUE(series,year,serial), CHECK(state NOT IN ('issued','reversed') OR (number IS NOT NULL AND issued_at IS NOT NULL AND due_on IS NOT NULL))
);
CREATE TABLE invoice_allocations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), invoice_id uuid NOT NULL REFERENCES invoices(id), session_id uuid NOT NULL REFERENCES work_sessions(id), source_version bigint NOT NULL,
 started_at timestamptz NOT NULL, stopped_at timestamptz NOT NULL CHECK(stopped_at>started_at), active boolean NOT NULL DEFAULT true,
 net_us bigint NOT NULL CHECK(net_us>=0), net_cents bigint NOT NULL CHECK(net_cents>=0), snapshot jsonb NOT NULL,
 EXCLUDE USING gist(session_id WITH =, tstzrange(started_at,stopped_at,'[)') WITH &&) WHERE(active)
);
CREATE INDEX invoice_allocations_invoice ON invoice_allocations(invoice_id);
CREATE TABLE payments (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), invoice_id uuid NOT NULL REFERENCES invoices(id), amount_cents bigint NOT NULL CHECK(amount_cents>0), paid_on date NOT NULL, reference text NOT NULL DEFAULT '', actor_id uuid NOT NULL REFERENCES users(id), created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE invoice_credits (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), invoice_id uuid NOT NULL UNIQUE REFERENCES invoices(id), number text NOT NULL UNIQUE, net_cents bigint NOT NULL, tax_cents bigint NOT NULL, total_cents bigint NOT NULL, reason text NOT NULL, actor_id uuid NOT NULL REFERENCES users(id), created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE FUNCTION protect_invoice_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF OLD.state IN ('issued','reversed') AND (NEW.issuer IS DISTINCT FROM OLD.issuer OR NEW.customer IS DISTINCT FROM OLD.customer OR NEW.net_cents<>OLD.net_cents OR NEW.tax_cents<>OLD.tax_cents OR NEW.total_cents<>OLD.total_cents OR NEW.number IS DISTINCT FROM OLD.number OR NEW.currency<>OLD.currency OR NEW.timezone<>OLD.timezone OR NEW.farm_id<>OLD.farm_id OR NEW.from_date<>OLD.from_date OR NEW.to_date<>OLD.to_date OR NEW.period_start<>OLD.period_start OR NEW.period_stop<>OLD.period_stop OR NEW.issued_at IS DISTINCT FROM OLD.issued_at OR NEW.due_on IS DISTINCT FROM OLD.due_on OR NEW.state NOT IN ('issued','reversed')) THEN RAISE EXCEPTION 'Issued invoice content is immutable' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER invoice_snapshot_immutable BEFORE UPDATE ON invoices FOR EACH ROW EXECUTE FUNCTION protect_invoice_snapshot();
