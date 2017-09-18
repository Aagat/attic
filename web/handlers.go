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

	if b.Url == "" {
		http.Error(w, "URL can't be empty", http.StatusBadRequest)
		return
	}

	b.FillMissing()
	err = b.Insert()

	if err != nil {
		log.Fatal(err)
	}

	persisted, err := a.db.GetBookmarkByHash(b.Hash)

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(persisted)
}

func (a *App) UpdateBookmarkById(w http.ResponseWriter, r *http.Request) {

	decoder := json.NewDecoder(r.Body)

	var b models.Bookmark
	err := decoder.Decode(&b)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Body.Close()

	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	stored, err := a.db.GetBookmarkById(id)

	if err != nil {
		// Proper assertion required
		http.Error(w, "404", http.StatusNotFound)
		return
	}

	b.Id = id

	if b.Url != stored.Url {
		b.CalculateHash()
	}

	err = a.db.UpdateBookmarkById(&b)

	if err != nil {
		// Proper assertion required
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Println(err)
		return
	}

	persisted, err := a.db.GetBookmarkById(b.Id)

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(persisted)
}

func (a *App) UpdateBookmarkByHash(w http.ResponseWriter, r *http.Request) {

	decoder := json.NewDecoder(r.Body)

	var b models.Bookmark
	err := decoder.Decode(&b)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Body.Close()

	stored, err := a.db.GetBookmarkByHash(mux.Vars(r)["hash"])

	if err != nil {
		http.Error(w, "404", http.StatusNotFound)
		return
	}

	b.Id = stored.Id
	b.Hash = mux.Vars(r)["hash"]

	if b.Url != stored.Url {
		// Hash mismatch
		http.Error(w, "You can't change URL when updating by hash", http.StatusBadRequest)
		return
	}

	err = a.db.UpdateBookmarkByHash(&b)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Println(err)
		return
	}

	persisted, err := a.db.GetBookmarkByHash(b.Hash)

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(persisted)
}
