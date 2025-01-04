package api

import (
	"github.com/aagat/attic/backend/config"
	"github.com/gorilla/mux"
)

// SetupRoutes configures and returns the router with all API routes
func SetupRoutes(cfg *config.Config) (*mux.Router, error) {
	r := mux.NewRouter()

	// Add middleware
	r.Use(loggingMiddleware)

	// API routes
	r.HandleFunc("/add-to-kindle", AddToKindleHandler).Methods("POST")

	return r, nil
}
