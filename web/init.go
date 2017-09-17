package web

import (
	"github.com/aagat/attic/models"
)

type App struct {
	db *models.DB
}

func NewApp(db *models.DB) (*App, error) {
	a := &App{db: db}
	return a, nil
}
