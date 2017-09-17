package main

import (
	"database/sql"
	"flag"
	"github.com/aagat/attic/helpers"
	"github.com/aagat/attic/models"
	"github.com/aagat/attic/web"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"net/http"
)

var importFromFile = flag.String("import", "", "Bookmark file to import from")

func main() {

	flag.Parse()

	db, err := sql.Open("sqlite3", "/home/aagat/bookmarks.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	models.DB = db
	app, _ := web.NewApp(db)
	utils, _ := helpers.Init(db)

	err = utils.CreateTables()
	if err != nil {
		log.Fatal(err)
	}

	if *importFromFile != "" {
		utils.ImportBookmarks(importFromFile)
	}

	http.HandleFunc("/", app.Index)

	log.Println("Listening and serving on port 8000")
	http.ListenAndServe(":8000", nil)
}
