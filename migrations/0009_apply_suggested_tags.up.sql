-- Apply existing classifications while preserving all chosen tags and their order.
UPDATE saved_items
SET tags = (
  SELECT coalesce(jsonb_agg(value ORDER BY first_position), '[]'::jsonb)
  FROM (
    SELECT value, min(position) AS first_position
    FROM jsonb_array_elements(tags || suggested_tags) WITH ORDINALITY AS entry(value, position)
    GROUP BY value
  ) merged
), version = version + 1, updated_at = now()
WHERE NOT tags @> suggested_tags;
