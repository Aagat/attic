package main

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"net/http"
	"time"
)

type Tag string

type Bookmark struct {
	Id           int       `json:"id"`
	Created      time.Time `json:"created"`
	LastUpdated  time.Time `json:"last_updated"`
	LastVerified time.Time `json:"last_verified"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Url          string    `json:"url"`
	Hash         string    `json:"hash"`
	Tags         []Tag     `json:"tags"`
	Alive        bool      `json:"alive"`
	Archived     bool      `json:"archived"`
}

func main() {

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	bootstrapTable := `
CREATE TABLE IF NOT EXISTS bookmarks (
  id INTEGER PRIMARY KEY,
  created DATETIME,
  updated DATETIME,
  verified DATETIME,
  title TEXT,
  description TEXT,
  url TEXT,
  hash TEXT,
  tags TEXT,
  alive TINYINT,
  archived TINYINT
);

CREATE TABLE IF NOT EXISTS tags (
  id INTEGER PRIMARY KEY,
  bookmark_id INTEGER,
  tag TEXT
);

CREATE INDEX IF NOT EXISTS urlhash ON bookmarks (hash);
`
	_, err = db.Exec(bootstrapTable)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello world"))
	})

	log.Println("Listening and serving on port 8000")
	http.ListenAndServe(":8000", nil)
}
