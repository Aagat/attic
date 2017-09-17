package main

import (
	"log"
	"net/http"
	"time"
)

type Tag string

type Bookmark struct {
	Created      time.Time `json:"created"`
	LastUpdated  time.Time `json:"last_updated"`
	LastVerified time.Time `json:"last_verified"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Url          string    `json:"url"`
	UrlHash      string    `json:"id"`
	Tags         []Tag     `json:"tags"`
	Alive        bool      `json:"alive"`
	Archived     bool      `json:"archived"`
}

func main() {

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello world"))
	})

	log.Println("Listening and serving on port 8000")
	http.ListenAndServe(":8000", nil)
}
