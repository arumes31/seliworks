CREATE TABLE invoice_deliveries (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), invoice_id uuid NOT NULL UNIQUE REFERENCES invoices(id),
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','sending','retry','sent','failed','uncertain','cancelled')),
 sender text NOT NULL, recipient text NOT NULL, message_id text NOT NULL UNIQUE, pdf_path text NOT NULL, pdf_sha256 text NOT NULL,
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5), total_attempts integer NOT NULL DEFAULT 0 CHECK(total_attempts>=attempts),
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(), lease_token text, lease_until timestamptz,
 last_error text NOT NULL DEFAULT '', created_by uuid NOT NULL REFERENCES users(id), created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp(), sent_at timestamptz,
 CHECK(state<>'sending' OR (lease_token IS NOT NULL AND lease_until IS NOT NULL))
);
CREATE INDEX invoice_deliveries_ready ON invoice_deliveries(next_attempt_at,created_at) WHERE state IN ('queued','retry');
CREATE TABLE invoice_delivery_attempts (
 delivery_id uuid NOT NULL REFERENCES invoice_deliveries(id), attempt integer NOT NULL, lease_token text NOT NULL UNIQUE,
 state text NOT NULL CHECK(state IN ('sending','sent','retry','failed','uncertain','cancelled')), error_code text NOT NULL DEFAULT '', started_at timestamptz NOT NULL DEFAULT clock_timestamp(), finished_at timestamptz,
 PRIMARY KEY(delivery_id,attempt)
);
