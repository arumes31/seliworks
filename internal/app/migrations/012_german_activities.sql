-- Translate untouched defaults only; keep customized names and identities.
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
WHERE a.name = names.english AND a.version = 1;
