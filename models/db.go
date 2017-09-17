package models

import (
	"database/sql"
)

// Global reference for use within the package.
var dbg *sql.DB

type DB struct {
	DB *sql.DB
}

func NewDB(db *sql.DB) (*DB, error) {
	dbg = db
	return &DB{DB: db}, nil
}
