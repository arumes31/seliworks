-- Application-created/edited activities always have a transactional, immutable
-- activities.save audit event. Seed rows from 001_core.sql have none.
UPDATE activities AS a
SET name = names.german, version = a.version + 1
FROM (VALUES
 ('Feeding animals', 'Tiere füttern'),
 ('Stable work', 'Stallarbeit'),
 ('Field work', 'Feldarbeit'),
 ('Harvesting', 'Erntearbeit'),
 ('Machinery maintenance', 'Maschinenwartung'),
 ('Other', 'Sonstiges')
) AS names(english, german)
WHERE a.name = names.english AND a.version = 1
AND NOT EXISTS (
 SELECT 1 FROM audit_events AS event
 WHERE event.action = 'activities.save' AND event.entity_id = a.id::text
);
