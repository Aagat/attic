package web

import (
	"github.com/aagat/attic/config"
	"github.com/aagat/attic/fetcher"
	"github.com/aagat/attic/models"
)

type Handler struct {
	db      *models.Models
	fetcher *fetcher.Fetcher
}

func Init(c *config.Config) *Handler {
	a := &Handler{
		db:      c.Models.(*models.Models),
		fetcher: c.Fetcher.(*fetcher.Fetcher),
	}
	return a
}
