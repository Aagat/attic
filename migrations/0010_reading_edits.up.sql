CREATE TABLE reading_edits (
 job_id text PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
 draft jsonb NOT NULL
);
