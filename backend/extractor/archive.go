package extractor

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/aagat/attic/backend/logger"
	"github.com/rs/zerolog"
)

// ArchiveExtractor defines the interface for archive-based content extraction
type ArchiveExtractor interface {
	getArchiveURL(url string) (string, error)
}

type archiveResponse struct {
	ArchivedSnapshots struct {
		Closest struct {
			Available bool   `json:"available"`
			URL       string `json:"url"`
		} `json:"closest"`
	} `json:"archived_snapshots"`
}

type archiveExtractor struct {
	client *http.Client
	log    zerolog.Logger
}

func newArchiveExtractor() *archiveExtractor {
	return &archiveExtractor{
		client: &http.Client{},
		log:    logger.WithComponent("archive_extractor"),
	}
}

func (a *archiveExtractor) getArchiveURL(targetURL string) (string, error) {
	a.log.Info().Str("url", targetURL).Msg("Checking Internet Archive for content")

	// Query archive.org API
	apiURL := fmt.Sprintf("https://archive.org/wayback/available?url=%s", url.QueryEscape(targetURL))
	req, err := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
	if err != nil {
		a.log.Error().Err(err).Str("url", apiURL).Msg("Failed to create archive request")
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		a.log.Error().Err(err).Str("url", apiURL).Msg("Failed to query archive")
		return "", fmt.Errorf("failed to query archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		a.log.Error().Int("status", resp.StatusCode).Str("url", apiURL).Msg("Archive API returned error")
		return "", fmt.Errorf("archive API returned status %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		a.log.Error().Err(err).Str("url", apiURL).Msg("Failed to read archive response")
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var result archiveResponse
	if err := json.Unmarshal(body, &result); err != nil {
		a.log.Error().Err(err).Str("url", apiURL).Msg("Failed to parse archive response")
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.ArchivedSnapshots.Closest.Available {
		a.log.Warn().Str("url", targetURL).Msg("No archived version available")
		return "", fmt.Errorf("no archived version available")
	}

	archiveURL := result.ArchivedSnapshots.Closest.URL
	a.log.Info().Str("url", archiveURL).Msg("Found archived version")

	return archiveURL, nil
}
