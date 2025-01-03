package api

import (
	"github.com/aagat/attic/backend/config"
	"github.com/gorilla/mux"
)

// SetupRoutes configures and returns the router with all API routes
func SetupRoutes(cfg *config.Config) (*mux.Router, error) {
	// Create handler
	h, err := NewHandler(cfg)
	if err != nil {
		return nil, err
	}

	r := mux.NewRouter()

	// Add middleware
	r.Use(loggingMiddleware)

	// API routes
	r.HandleFunc("/add-to-kindle", h.AddToKindleHandler).Methods("POST")

	return r, nil
}
