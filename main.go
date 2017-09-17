package main

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"flag"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/net/html"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

type Tag struct {
	Id         int    `json:"-"`
	BookmarkID int    `json:"bookmark_id"`
	Tag        string `json:"tag"`
}

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

var importFromFile = flag.String("import", "", "Bookmark file to import from")

func main() {

	flag.Parse()

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

	if *importFromFile != "" {
		ImportBookmarks(importFromFile)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello world"))
	})

	log.Println("Listening and serving on port 8000")
	http.ListenAndServe(":8000", nil)
}

func ImportBookmarks(f *string) {

	b := []Bookmark{}

	err := BookmarksParser(f, &b)

	if err != nil {
		log.Fatal(err)
	}

	if err != nil {
		log.Fatal(err)
	}
}

func BookmarksParser(f *string, b *[]Bookmark) error {
	dat, err := os.Open(*f)
	defer dat.Close()
	if err != nil {
		log.Fatal(err)
	}

	z := html.NewTokenizer(dat)
	for {
		tt := z.Next()
		switch {
		case tt == html.ErrorToken:
			if z.Err() == io.EOF {
				log.Println("Bookmarks file parsed successfully. Total bookmarks parsed:", len(*b))
				return nil
			}
			return z.Err()
		case tt == html.StartTagToken:
			token := z.Token()

			isAnchor := token.Data == "a"
			if isAnchor {
				bookmark := Bookmark{
					Tags:  []Tag{},
					Alive: false,
				}
				for _, a := range token.Attr {
					if a.Key == "href" {

						bookmark.Url = a.Val

						hash := sha1.New()
						hash.Write([]byte(a.Val))

						bookmark.Hash = hex.EncodeToString(hash.Sum(nil))
					} else if a.Key == "add_date" {

						tm, err := strconv.ParseInt(a.Val, 10, 64)

						if err != nil {
							log.Fatal(err)
						}

						created := time.Unix(tm, 0)
						bookmark.Created = created
						bookmark.LastUpdated = created
						bookmark.LastVerified = created
					}
				}

				z.Next()
				bookmark.Title = z.Token().Data

				*b = append(*b, bookmark)
			}
		}

	}
}
