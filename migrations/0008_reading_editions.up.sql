CREATE TABLE reading_editions (
 job_id text PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
 item_id text NOT NULL REFERENCES saved_items(id) ON DELETE CASCADE,
 language text NOT NULL DEFAULT '' CHECK (language IN ('','en','es','fr','de','it','pt','nl','ja','ko','zh'))
);
CREATE INDEX reading_editions_item_language ON reading_editions(item_id,language);
