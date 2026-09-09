package processing_test

import (
	"attic/internal/domain"
	"context"
)

type outputLanguageFunc func(context.Context, domain.JobID) (string, error)

func (f outputLanguageFunc) OutputLanguage(ctx context.Context, id domain.JobID) (string, error) {
	return f(ctx, id)
}
