package extractor

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/aagat/attic/backend/logger"
	"github.com/rs/zerolog"
)

// ArchiveExtractor defines the interface for archive-based content extraction
type ArchiveExtractor interface {
	getArchiveURL(url string) (string, error)
}

// archiveSource represents a source for archived content
type archiveSource struct {
	name     string
	getURL   func(client *http.Client, url string) (string, error)
	priority int // Lower number means higher priority
}

type archiveExtractor struct {
	client  *http.Client
	log     zerolog.Logger
	sources []archiveSource
	index   int // Track which source we're trying
}

func newArchiveExtractor() *archiveExtractor {
	return &archiveExtractor{
		client: &http.Client{},
		log:    logger.WithComponent("archive_extractor"),
		index:  0,
		sources: []archiveSource{
			{
				name:     "archive.org",
				getURL:   getArchiveOrgURL,
				priority: 1,
			},
			{
				name:     "archive.today",
				getURL:   getArchiveTodayURL,
				priority: 2,
			},
		},
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

	archiveURL, err := source.getURL(a.client, targetURL)
	if err != nil {
		a.log.Debug().
			Err(err).
			Str("url", targetURL).
			Str("source", source.name).
			Msg("Archive source failed")
	} else {
		a.log.Info().
			Str("url", targetURL).
			Str("archive_url", archiveURL).
			Str("source", source.name).
			Msg("Found archived version")
	}

	// Move to next source for next call
	a.index++
	return archiveURL, err
}

// getArchiveOrgURL gets an archived URL from archive.org
func getArchiveOrgURL(client *http.Client, targetURL string) (string, error) {
	return fmt.Sprintf("https://web.archive.org/web/%s", url.QueryEscape(targetURL)), nil
}

// getArchiveTodayURL gets an archived URL from archive.today
func getArchiveTodayURL(client *http.Client, targetURL string) (string, error) {
	return fmt.Sprintf("https://archive.today/%s", url.QueryEscape(targetURL)), nil
}
