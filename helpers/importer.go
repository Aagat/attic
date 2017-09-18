package helpers

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"github.com/aagat/attic/config"
	"github.com/aagat/attic/models"
	"github.com/aagat/attic/search"
	"golang.org/x/net/html"
	"io"
	"log"
	"os"
	"strconv"
	"time"
)

type Helpers struct {
	db    *sql.DB
	index *search.Search
}

func Init(c *config.Config) *Helpers {
	return &Helpers{
		db:    c.DB.(*sql.DB),
		index: c.Search.(*search.Search),
	}
}

func (h *Helpers) ImportBookmarks(f *string) {

	b := []models.Bookmark{}

	err := h.BookmarksParser(f, &b)

	if err != nil {
		log.Fatal(err)
	}

	for _, val := range b {
		err = val.Insert()
		if err != nil {
			log.Fatal(err)
		}

		go h.index.Add(val.Hash, val)
	}

	if err != nil {
		log.Fatal(err)
	}
}

func (h *Helpers) BookmarksParser(f *string, b *[]models.Bookmark) error {
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
				bookmark := models.Bookmark{
					Tags:  []models.Tag{},
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
						bookmark.Updated = created
						bookmark.Verified = created
					}
				}

				z.Next()
				bookmark.Title = z.Token().Data

				*b = append(*b, bookmark)
			}
		}

	}
}
