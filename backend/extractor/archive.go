package extractor

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"

	"github.com/aagat/attic/backend/config"
	"github.com/aagat/attic/backend/logger"
	"github.com/expr-lang/expr"
	"github.com/rs/zerolog"
)

// ArchiveExtractor defines the interface for archive-based content extraction
type ArchiveExtractor interface {
	getArchiveURL(url string) (string, error)
}

// archiveSource represents a source for archived content
type archiveSource struct {
	name     string
	urlExpr  string
	priority int
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
			name:     archive.Name,
			urlExpr:  archive.URLExpr,
			priority: archive.Priority,
		}
	}

	// Sort sources by priority
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].priority < sources[j].priority
	})

	return &archiveExtractor{
		client:  &http.Client{},
		log:     logger.WithComponent("archive_extractor"),
		sources: sources,
		index:   0,
	}
}

func (a *archiveExtractor) getArchiveURL(targetURL string) (string, error) {
	if a.index >= len(a.sources) {
		return "", fmt.Errorf("no more archive sources available")
	}

	source := a.sources[a.index]
	a.log.Debug().
		Str("url", targetURL).
		Str("source", source.name).
		Int("attempt", a.index+1).
		Msg("Trying archive source")

	// Parse the target URL
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	// Create environment for expr with string functions
	env := map[string]interface{}{
		"url": map[string]interface{}{
			"scheme":     parsedURL.Scheme,
			"host":       parsedURL.Host,
			"path":       parsedURL.Path,
			"rawQuery":   parsedURL.RawQuery,
			"fragment":   parsedURL.Fragment,
			"rawURL":     targetURL,
			"encodedURL": url.QueryEscape(targetURL),
		},
		// Add string functions
		"join": func(parts ...string) string {
			result := ""
			for _, part := range parts {
				result += part
			}
			return result
		},
		"concat": func(parts ...string) string {
			result := ""
			for _, part := range parts {
				result += part
			}
			return result
		},
	}

	// Evaluate the URL expression
	program, err := expr.Compile(source.urlExpr, expr.Env(env))
	if err != nil {
		return "", fmt.Errorf("failed to compile URL expression: %w", err)
	}

	result, err := expr.Run(program, env)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate URL expression: %w", err)
	}

	archiveURL, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("URL expression did not return a string")
	}

	a.log.Info().
		Str("url", targetURL).
		Str("source", source.name).
		Str("archive_url", archiveURL).
		Msg("Found archived version")

	a.index++
	return archiveURL, nil
}
