package helpers

func (h *Helpers) CreateTables() error {
	bootstrapTable := `
CREATE TABLE IF NOT EXISTS bookmarks (
  id INTEGER PRIMARY KEY,
  created DATETIME,
  updated DATETIME,
  verified DATETIME,
  title TEXT,
  description TEXT,
  url TEXT,
  hash TEXT UNIQUE,
  tags TEXT,
  alive TINYINT,
  archived TINYINT
);

CREATE INDEX IF NOT EXISTS urlhash ON bookmarks (hash);
`
	_, err := h.db.Exec(bootstrapTable)
	if err != nil {
		return err
	}

	return nil
}
