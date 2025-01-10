package extractor

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"

	"github.com/aagat/attic/backend/config"
	"github.com/aagat/attic/backend/logger"
	"github.com/rs/zerolog"
)

// ArchiveExtractor defines the interface for archive-based content extraction
type ArchiveExtractor interface {
	getArchiveURL(url string) (string, error)
}

// archiveSource represents a source for archived content
type archiveSource struct {
	name      string
	urlFormat string
	priority  int
}

type archiveExtractor struct {
	client  *http.Client
	log     zerolog.Logger
	sources []archiveSource
	index   int // Track which source we're trying
}

func newArchiveExtractor(cfg []config.ArchiveConfig) *archiveExtractor {
	// Convert config to archive sources
	sources := make([]archiveSource, len(cfg))
	for i, archive := range cfg {
		sources[i] = archiveSource{
			name:      archive.Name,
			urlFormat: archive.URLFormat,
			priority:  archive.Priority,
		}
	}

	// Sort sources by priority
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].priority < sources[j].priority
	})

	return &archiveExtractor{
		client:  &http.Client{},
		log:     logger.WithComponent("archive_extractor"),
		index:   0,
		sources: sources,
	}
}

func (a *archiveExtractor) getArchiveURL(targetURL string) (string, error) {
	// If we've tried all sources, return error
	if a.index >= len(a.sources) {
		a.index = 0 // Reset for next time
		return "", fmt.Errorf("no more archive sources available")
	}

	source := a.sources[a.index]
	a.log.Debug().
		Str("url", targetURL).
		Str("source", source.name).
		Int("attempt", a.index+1).
		Msg("Trying archive source")

	// Format the archive URL using the configured format string
	archiveURL := fmt.Sprintf(source.urlFormat, url.QueryEscape(targetURL))
	a.log.Info().
		Str("url", targetURL).
		Str("archive_url", archiveURL).
		Str("source", source.name).
		Msg("Found archived version")

	// Move to next source for next call
	a.index++
	return archiveURL, nil
}
