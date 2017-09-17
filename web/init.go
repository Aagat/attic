package web

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
)

type App struct {
	db *sql.DB
}

func NewApp(db *sql.DB) (*App, error) {
	a := &App{db: db}
	return a, nil
}
