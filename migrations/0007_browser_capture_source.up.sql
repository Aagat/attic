ALTER TABLE saved_captures ADD COLUMN source text NOT NULL DEFAULT 'server' CHECK (source IN ('server','browser'));
