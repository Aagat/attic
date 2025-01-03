package main

import (
	"log"
	"net/http"

	"github.com/aagat/attic/backend/api"
	"github.com/aagat/attic/backend/config"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize router
	router, err := api.SetupRoutes(cfg)
	if err != nil {
		log.Fatalf("Failed to setup routes: %v", err)
	}

	// Start server
	log.Printf("Starting server on port %s", cfg.ServerPort)
	if err := http.ListenAndServe(":"+cfg.ServerPort, router); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
