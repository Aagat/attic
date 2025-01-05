package logger

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type contextKey string

const loggerKey = contextKey("logger")

func init() {
	// Configure zerolog
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: "2006/01/02 15:04:05",
	})
}

// Logger returns the global logger instance
func Logger() zerolog.Logger {
	return log.Logger
}

// WithRequestID returns a logger with the request ID field
func WithRequestID(requestID string) zerolog.Logger {
	return Logger().With().Str("request_id", requestID).Logger()
}

// WithComponent returns a logger with the component field
func WithComponent(component string) zerolog.Logger {
	return Logger().With().Str("component", component).Logger()
}

// WithContext stores the logger in the context
func WithContext(ctx context.Context, logger zerolog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext retrieves the logger from the context
func FromContext(ctx context.Context) zerolog.Logger {
	if logger, ok := ctx.Value(loggerKey).(zerolog.Logger); ok {
		return logger
	}
	return Logger()
}
