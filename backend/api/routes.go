package api

import (
	"github.com/gorilla/mux"
)

// SetupRoutes configures and returns the router with all API routes
func SetupRoutes() *mux.Router {
	r := mux.NewRouter()

	// Add middleware
	r.Use(loggingMiddleware)

	// API routes
	r.HandleFunc("/add-to-kindle", AddToKindleHandler).Methods("POST")

	return r
}
