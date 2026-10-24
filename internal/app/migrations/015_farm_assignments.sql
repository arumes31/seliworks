-- Existing farms keep their explicit memberships; new API-created farms default to all.
ALTER TABLE farms ADD COLUMN assign_all_workers boolean NOT NULL DEFAULT false;
CREATE INDEX worker_farms_farm ON worker_farms(farm_id, worker_id);
