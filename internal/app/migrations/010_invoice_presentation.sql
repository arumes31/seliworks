ALTER TABLE organization
 ADD COLUMN tax_identifier text NOT NULL DEFAULT '' CHECK(length(tax_identifier)<=200),
 ADD COLUMN invoice_footer text NOT NULL DEFAULT '' CHECK(length(invoice_footer)<=2000),
 ADD COLUMN invoice_layout text NOT NULL DEFAULT 'entries' CHECK(invoice_layout IN('entries','activity_rate')),
 ADD COLUMN invoice_appendix boolean NOT NULL DEFAULT false;
