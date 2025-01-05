package api

import (
	"net/http"

	"github.com/aagat/attic/backend/config"
)

// SetupRoutes configures all application routes
func SetupRoutes(cfg *config.Config) (http.Handler, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/add-to-kindle", AddToKindleHandler)

	// Wrap with middleware
	handler := loggingMiddleware(mux)

	return handler, nil
}
