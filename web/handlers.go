package web

import (
	"encoding/json"
	"log"
	"net/http"
)

func (a *App) Index(w http.ResponseWriter, r *http.Request) {

	bookmarks, err := a.db.GetAllBookmarks()

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(bookmarks)
}
