package main

import (
	"database/sql"
	"flag"
	"github.com/aagat/attic/helpers"
	"github.com/aagat/attic/models"
	"github.com/aagat/attic/search"
	"github.com/aagat/attic/web"
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

	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	models, _ := models.NewDB(db)
	index, _ := search.OpenIndex(*indexPath)
	app, _ := web.NewApp(models)
	utils, _ := helpers.Init(db, index)

	err = utils.CreateTables()
	if err != nil {
		log.Fatal(err)
	}

	if *importFromFile != "" {
		utils.ImportBookmarks(importFromFile)
	}

	http.HandleFunc("/", app.Index)

	log.Printf("Listening and serving on port %v\n", *addr)
	http.ListenAndServe(*addr, nil)
}
