-- Prefer a usable existing document when a newer failed attempt shares its URL.
UPDATE saved_items i SET job_id=best.id,
 title=CASE WHEN i.title_edited THEN i.title ELSE COALESCE(best.title,i.title) END,
 text_content=CASE WHEN i.text_content='' THEN COALESCE(best.plain_text,'') ELSE i.text_content END,
 version=i.version+1
FROM (
 SELECT DISTINCT ON (j.submitted_url) j.submitted_url,j.id,c.title,c.plain_text
 FROM jobs j JOIN artifacts a ON a.job_id=j.id AND a.availability='available'
 LEFT JOIN content_documents c ON c.job_id=j.id
 ORDER BY j.submitted_url,j.created_at DESC
) best
WHERE i.url=best.submitted_url AND i.job_id<>best.id
 AND NOT EXISTS(SELECT 1 FROM artifacts WHERE job_id=i.job_id AND availability='available')
 AND NOT EXISTS(SELECT 1 FROM jobs WHERE id=i.job_id AND status IN ('queued','processing','delivering'));
