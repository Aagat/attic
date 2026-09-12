ALTER TABLE saved_items ADD COLUMN enrichment_error text NOT NULL DEFAULT '';
-- Configuration becoming available must not submit old pending mail.
ALTER TABLE jobs ADD COLUMN delivery_paused boolean NOT NULL DEFAULT false;
