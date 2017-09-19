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

CREATE TABLE IF NOT EXISTS bookmarks_meta (
  id INTEGER PRIMARY KEY,
  created DATETIME,
  bookmark TEXT UNIQUE,
  title TEXT,
  description TEXT,
  keywords TEXT,
  type TEXT
);

CREATE INDEX IF NOT EXISTS urlhash ON bookmarks (hash);
CREATE INDEX IF NOT EXISTS urlhash ON bookmarks_meta (bookmark);
`
	_, err := h.db.Exec(bootstrapTable)
	if err != nil {
		return err
	}

	return nil
}
