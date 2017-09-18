package web

import (
	"github.com/aagat/attic/config"
	"github.com/aagat/attic/models"
)

type App struct {
	db *models.DB
}

func NewApp(c *config.Config) *App {
	a := &App{db: c.Models.(*models.DB)}
	return a
}
