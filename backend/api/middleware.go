package api

import (
	"net/http"
	"time"

	"github.com/aagat/attic/backend/logger"
	"github.com/google/uuid"
)

// loggingMiddleware adds request logging and tracking
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate request ID
		requestID := uuid.New().String()

		// Create request-scoped logger
		log := logger.WithRequestID(requestID)

		// Log request start
		log.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote_addr", r.RemoteAddr).
			Msg("Request started")

		start := time.Now()

		// Store logger in request context
		ctx := logger.WithContext(r.Context(), log)
		r = r.WithContext(ctx)

		// Create response wrapper to capture status code
		rw := newResponseWriter(w)

		// Process request
		next.ServeHTTP(rw, r)

		// Log request completion
		duration := time.Since(start)
		log.Info().
			Int("status", rw.status).
			Dur("duration", duration).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Msg("Request completed")
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	status int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write implements http.ResponseWriter
func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.status == 0 {
		rw.status = http.StatusOK
	}
	return rw.ResponseWriter.Write(b)
}
