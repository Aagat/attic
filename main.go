package main

import (
	"database/sql"
	"flag"
	"github.com/aagat/attic/config"
	"github.com/aagat/attic/helpers"
	"github.com/aagat/attic/models"
	"github.com/aagat/attic/search"
	"github.com/aagat/attic/web"
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"net/http"
)

var addr = flag.String("listen", ":8000", "HTTP ip/port to listen to")
var dbPath = flag.String("db", ":memory:", "Database file to use")
var indexPath = flag.String("index", "attic.index", "Database file to use")
var importFromFile = flag.String("import", "", "Bookmark file to import from")

func main() {

	flag.Parse()

	var c config.Config

	r := mux.NewRouter()

	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	c.DB = db

	c.Models, _ = models.NewDB(&c)

	c.Search, err = search.OpenIndex(*indexPath)

	if err != nil {
		log.Fatal(err)
	}

	c.Web = web.NewApp(&c)
	app := c.Web.(*web.App)
	c.Helpers = helpers.Init(&c)

	err = c.Helpers.(*helpers.Config).CreateTables()
	if err != nil {
		log.Fatal(err)
	}

	if *importFromFile != "" {
		c.Helpers.(*helpers.Config).ImportBookmarks(importFromFile)
	}

	r.HandleFunc("/", app.Index)
	r.HandleFunc("/show/{id:[0-9]+}", app.BookmarkById).Methods("GET")
	r.HandleFunc("/show/{hash}", app.BookmarkByHash).Methods("GET")
	r.HandleFunc("/add", app.NewBookmark).Methods("POST")
	r.HandleFunc("/update/{id:[0-9]+}", app.UpdateBookmarkById).Methods("POST")
	r.HandleFunc("/update/{hash}", app.UpdateBookmarkByHash).Methods("POST")

	log.Printf("Listening and serving on port %v\n", *addr)
	http.ListenAndServe(*addr, r)
}
