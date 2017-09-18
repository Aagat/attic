package web

import (
	"github.com/aagat/attic/config"
	"github.com/aagat/attic/models"
)

type Handler struct {
	db *models.Models
}

func Init(c *config.Config) *Handler {
	a := &Handler{db: c.Models.(*models.Models)}
	return a
}
