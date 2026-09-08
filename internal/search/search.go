// Package search keeps retrieval independent of canonical storage and indexing jobs.
package search

import (
	"context"
	"errors"
	"time"
)

var (
	ErrUnavailable = errors.New("search unavailable")
	ErrInvalid     = errors.New("invalid search request")
)

// Document is a disposable projection of a saved item, never its canonical record.
type Document struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	URL           string    `json:"url"`
	Notes         string    `json:"notes"`
	Tags          []string  `json:"tags"`
	Text          string    `json:"text"`
	Domain        string    `json:"domain"`
	Kind          string    `json:"kind"`
	CaptureStatus string    `json:"capture_status"`
	SavedAt       time.Time `json:"saved_at"`
}

// Query combines all filters with AND; each tag must match. Date bounds are inclusive.
// Limit defaults to 20 and cannot exceed 100. Offset must be nonnegative.
type Query struct {
	Text          string
	Tags          []string
	Domain        string
	From, To      *time.Time
	CaptureStatus string
	Limit, Offset int
}

type Hit struct {
	Document Document `json:"document"`
	Snippet  string   `json:"snippet"` // Plain text, including any literal HTML in the source.
}

type Result struct {
	Hits           []Hit `json:"hits"`
	Total          int   `json:"total"`
	TotalEstimated bool  `json:"total_estimated"`
}

// Index mutations return nil only after the index has applied the change. A timeout
// is ambiguous: callers must retain dirty state and retry the idempotent mutation.
// Canonical storage owns ordering and prevents older updates overtaking newer ones.
type Index interface {
	Upsert(context.Context, Document) error
	Delete(context.Context, string) error
	Search(context.Context, Query) (Result, error)
}
