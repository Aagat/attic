CREATE TABLE saved_items (
 id text PRIMARY KEY,
 kind text NOT NULL CHECK(kind IN ('bookmark','pdf')),
 url text UNIQUE,
 title text NOT NULL DEFAULT '', notes text NOT NULL DEFAULT '', tags jsonb NOT NULL DEFAULT '[]',
 source_title text NOT NULL DEFAULT '', folders jsonb NOT NULL DEFAULT '[]',
 title_edited boolean NOT NULL DEFAULT false,
 saved_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 text_content text NOT NULL DEFAULT '', classification text NOT NULL DEFAULT '', suggested_tags jsonb NOT NULL DEFAULT '[]',
 enrichment_status text NOT NULL DEFAULT 'pending',
 capture_status text NOT NULL DEFAULT 'queued', capture_token text, capture_until timestamptz,
 capture_attempts integer NOT NULL DEFAULT 0, capture_next timestamptz NOT NULL DEFAULT now(),
 job_id text REFERENCES jobs(id) ON DELETE SET NULL,
 version bigint NOT NULL DEFAULT 1, indexed_version bigint NOT NULL DEFAULT 0,
 index_error boolean NOT NULL DEFAULT false
);
CREATE TABLE saved_captures (
 id text PRIMARY KEY, item_id text NOT NULL REFERENCES saved_items(id) ON DELETE CASCADE,
 artifact jsonb NOT NULL, final_url text NOT NULL, title text NOT NULL, text_content text NOT NULL,
 status text NOT NULL, missing jsonb NOT NULL DEFAULT '[]', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX saved_captures_item_idx ON saved_captures(item_id,created_at DESC);
CREATE TABLE bookmark_sources (
 client_id text NOT NULL,node_id text NOT NULL,item_id text REFERENCES saved_items(id) ON DELETE SET NULL,
 url text NOT NULL,title text NOT NULL DEFAULT '',folder text NOT NULL DEFAULT '',saved_at timestamptz,
 PRIMARY KEY(client_id,node_id)
);
CREATE TABLE saved_tombstones (url_hash text PRIMARY KEY, deleted_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE search_deletions (id text PRIMARY KEY);
CREATE TABLE item_actions (key text PRIMARY KEY,item_id text NOT NULL REFERENCES saved_items(id) ON DELETE CASCADE,created_at timestamptz NOT NULL DEFAULT now());
-- Adopt the newest existing job per submitted URL; retain all existing jobs/PDFs.
INSERT INTO saved_items(id,kind,url,title,saved_at,updated_at,text_content,job_id,capture_status,enrichment_status)
SELECT DISTINCT ON(j.submitted_url) j.id,'bookmark',j.submitted_url,COALESCE(c.title,j.display_title,j.title_hint,''),
j.created_at,j.updated_at,COALESCE(c.plain_text,''),j.id,'not_captured','pending'
FROM jobs j LEFT JOIN content_documents c ON c.job_id=j.id
WHERE j.submitted_url IS NOT NULL ORDER BY j.submitted_url,j.created_at DESC;
CREATE INDEX saved_items_capture_queue_idx ON saved_items(capture_next) WHERE capture_status IN ('queued','capturing');
-- PDF approval can supply searchable content even when independent capture failed.
CREATE FUNCTION attic_index_approved_item() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE saved_items SET text_content=CASE WHEN text_content='' THEN NEW.plain_text ELSE text_content END,
 title=CASE WHEN title_edited THEN title ELSE NEW.title END,version=version+1
 WHERE job_id=NEW.job_id;
 RETURN NEW;
END;
$$;
CREATE TRIGGER saved_item_approved_content AFTER INSERT ON content_documents FOR EACH ROW EXECUTE FUNCTION attic_index_approved_item();
