package web

import (
	"encoding/json"
	"github.com/aagat/attic/models"
	"log"
	"net/http"
)

func (a *App) Index(w http.ResponseWriter, r *http.Request) {

	bookmarks := []models.Bookmark{}

	rows, err := a.db.Query("select * from bookmarks")

	if err != nil {
		log.Fatal(err)
	}

	defer rows.Close()

	for rows.Next() {
		b := models.Bookmark{}
		err := rows.Scan(
			&b.Id,
			&b.Created,
			&b.Updated,
			&b.Verified,
			&b.Title,
			&b.Description,
			&b.Url,
			&b.Hash,
			&b.Alive,
			&b.Archived)

		if err != nil {
			log.Fatal(err)
		}

		err = rows.Err()
		if err != nil {
			log.Fatal(err)
		}

		bookmarks = append(bookmarks, b)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(bookmarks)
}
