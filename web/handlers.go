package web

import (
	"encoding/json"
	"github.com/aagat/attic/models"
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"strconv"
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

func (a *App) BookmarkById(w http.ResponseWriter, r *http.Request) {

	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	bookmark, err := a.db.GetBookmarkById(id)

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(bookmark)
}

func (a *App) BookmarkByHash(w http.ResponseWriter, r *http.Request) {

	bookmark, err := a.db.GetBookmarkByHash(mux.Vars(r)["hash"])

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(bookmark)
}

func (a *App) NewBookmark(w http.ResponseWriter, r *http.Request) {

	decoder := json.NewDecoder(r.Body)

	var b models.Bookmark
	err := decoder.Decode(&b)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Body.Close()

	if b.Url != "" {
		b.FillMissing()
		err = b.Insert()

		if err != nil {
			log.Fatal(err)
		}
	}

	persisted, err := a.db.GetBookmarkByHash(b.Hash)

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(persisted)
}
