package library

import (
	"context"
	"strings"
)

// Suggestions come from canonical saved items, including items not yet indexed.
func (l *Library) Suggestions(ctx context.Context, field, query string) ([]string, error) {
	var projection string
	switch field {
	case "tag":
		projection = `SELECT DISTINCT jsonb_array_elements_text(tags) AS value FROM saved_items`
	case "source":
		projection = `SELECT DISTINCT trim(both '[]' from substring(url from '^https?://(\[[^]]+\]|[^/:?#]+)')) AS value FROM saved_items WHERE url IS NOT NULL`
	default:
		return nil, ErrInvalid
	}
	if len(query) > 1000 {
		return nil, ErrInvalid
	}
	rows, err := l.db.QueryContext(ctx, `SELECT value FROM (`+projection+`) choices WHERE value<>'' AND strpos(lower(value),lower($1))>0 ORDER BY value LIMIT 20`, strings.TrimSpace(query))
	if err != nil {
		return nil, safe(err)
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, safe(err)
		}
		values = append(values, value)
	}
	return values, safe(rows.Err())
}
