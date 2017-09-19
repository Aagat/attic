package web

import (
	"encoding/json"
	"github.com/aagat/attic/models"
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"strconv"
)

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {

	bookmarks, err := h.db.GetAllBookmarks()

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(bookmarks)
}

func (h *Handler) BookmarkById(w http.ResponseWriter, r *http.Request) {

	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	bookmark, err := h.db.GetBookmarkById(id)

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(bookmark)
}

func (h *Handler) BookmarkByHash(w http.ResponseWriter, r *http.Request) {

	bookmark, err := h.db.GetBookmarkByHash(mux.Vars(r)["hash"])

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(bookmark)
}

func (h *Handler) NewBookmark(w http.ResponseWriter, r *http.Request) {

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
		log.Printf("%+v\n", b)
		log.Fatal(err)
	}

	persisted, err := h.db.GetBookmarkByHash(b.Hash)

	if err != nil {
		log.Fatal(err)
	}

	h.fetcher.Fetch(persisted.Url)

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(persisted)
}

func (h *Handler) UpdateBookmarkById(w http.ResponseWriter, r *http.Request) {

	decoder := json.NewDecoder(r.Body)

	var b models.Bookmark
	err := decoder.Decode(&b)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Body.Close()

	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	stored, err := h.db.GetBookmarkById(id)

	if err != nil {
		// Proper assertion required
		http.Error(w, "404", http.StatusNotFound)
		return
	}

	b.Id = id

	if b.Url != stored.Url {
		b.CalculateHash()
	}

	err = h.db.UpdateBookmarkById(&b)

	if err != nil {
		// Proper assertion required
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Println(err)
		return
	}

	persisted, err := h.db.GetBookmarkById(b.Id)

	if err != nil {
		log.Fatal(err)
	}

	h.fetcher.Fetch(persisted.Url)

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(persisted)
}

func (h *Handler) UpdateBookmarkByHash(w http.ResponseWriter, r *http.Request) {

	decoder := json.NewDecoder(r.Body)

	var b models.Bookmark
	err := decoder.Decode(&b)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Body.Close()

	stored, err := h.db.GetBookmarkByHash(mux.Vars(r)["hash"])

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

	err = h.db.UpdateBookmarkByHash(&b)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Println(err)
		return
	}

	persisted, err := h.db.GetBookmarkByHash(b.Hash)

	h.fetcher.Fetch(persisted.Url)

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(persisted)
}

func (h *Handler) DeleteBookmarkById(w http.ResponseWriter, r *http.Request) {

	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	err := h.db.DeleteBookmarkById(id)

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(id)
}

func (h *Handler) DeleteBookmarkByHash(w http.ResponseWriter, r *http.Request) {

	err := h.db.DeleteBookmarkByHash(mux.Vars(r)["hash"])

	if err != nil {
		log.Fatal(err)
	}

	resp := json.NewEncoder(w)
	resp.SetIndent("", "\t")
	err = resp.Encode(mux.Vars(r)["hash"])
}
