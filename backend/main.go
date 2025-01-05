package main

import (
	"net/http"

	"github.com/aagat/attic/backend/api"
	"github.com/aagat/attic/backend/config"
	"github.com/aagat/attic/backend/logger"
)

func main() {
	log := logger.WithComponent("server")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Create router
	router, err := api.SetupRoutes(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to setup routes")
	}

	// Start server
	addr := ":" + cfg.ServerPort
	log.Info().Str("port", cfg.ServerPort).Msg("Starting server")
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal().Err(err).Str("addr", addr).Msg("Server failed to start")
	}
}
