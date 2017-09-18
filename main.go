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

	var app config.Config

	r := mux.NewRouter()

	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	app.DB = db

	app.Models, _ = models.NewDB(&app)

	app.Search, err = search.OpenIndex(*indexPath)

	if err != nil {
		log.Fatal(err)
	}

	app.Web = web.NewApp(&app)
	handler := app.Web.(*web.App)
	app.Helpers = helpers.Init(&app)
	utils := app.Helpers.(*helpers.Config)

	err = utils.CreateTables()
	if err != nil {
		log.Fatal(err)
	}

	if *importFromFile != "" {
		utils.ImportBookmarks(importFromFile)
	}

	r.HandleFunc("/", handler.Index)
	r.HandleFunc("/show/{id:[0-9]+}", handler.BookmarkById).Methods("GET")
	r.HandleFunc("/show/{hash}", handler.BookmarkByHash).Methods("GET")
	r.HandleFunc("/add", handler.NewBookmark).Methods("POST")
	r.HandleFunc("/update/{id:[0-9]+}", handler.UpdateBookmarkById).Methods("POST")
	r.HandleFunc("/update/{hash}", handler.UpdateBookmarkByHash).Methods("POST")

	log.Printf("Listening and serving on port %v\n", *addr)
	http.ListenAndServe(*addr, r)
}
