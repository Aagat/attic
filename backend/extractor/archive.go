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

func (a *archiveExtractor) extract(ctx context.Context, targetURL string) ([]byte, error) {
	a.log.Info().Str("url", targetURL).Msg("Checking Internet Archive for content")

	// Query archive.org API
	apiURL := fmt.Sprintf("https://archive.org/wayback/available?url=%s", url.QueryEscape(targetURL))
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		a.log.Error().Err(err).Str("url", apiURL).Msg("Failed to create archive request")
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		a.log.Error().Err(err).Str("url", apiURL).Msg("Failed to query archive")
		return nil, fmt.Errorf("failed to query archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		a.log.Error().Int("status", resp.StatusCode).Str("url", apiURL).Msg("Archive API returned error")
		return nil, fmt.Errorf("archive API returned status %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		a.log.Error().Err(err).Str("url", apiURL).Msg("Failed to read archive response")
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result archiveResponse
	if err := json.Unmarshal(body, &result); err != nil {
		a.log.Error().Err(err).Str("url", apiURL).Msg("Failed to parse archive response")
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.ArchivedSnapshots.Closest.Available {
		a.log.Warn().Str("url", targetURL).Msg("No archived version available")
		return nil, fmt.Errorf("no archived version available")
	}

	// Get archived content
	archiveURL := result.ArchivedSnapshots.Closest.URL
	a.log.Info().Str("url", archiveURL).Msg("Found archived version")

	archiveReq, err := http.NewRequestWithContext(ctx, "GET", archiveURL, nil)
	if err != nil {
		a.log.Error().Err(err).Str("url", archiveURL).Msg("Failed to create archive content request")
		return nil, fmt.Errorf("failed to create archive request: %w", err)
	}

	archiveResp, err := a.client.Do(archiveReq)
	if err != nil {
		a.log.Error().Err(err).Str("url", archiveURL).Msg("Failed to get archived content")
		return nil, fmt.Errorf("failed to get archived content: %w", err)
	}
	defer archiveResp.Body.Close()

	if archiveResp.StatusCode != http.StatusOK {
		a.log.Error().Int("status", archiveResp.StatusCode).Str("url", archiveURL).Msg("Archive returned error")
		return nil, fmt.Errorf("archive returned status %d", archiveResp.StatusCode)
	}

	content, err := ioutil.ReadAll(archiveResp.Body)
	if err != nil {
		a.log.Error().Err(err).Str("url", archiveURL).Msg("Failed to read archived content")
		return nil, fmt.Errorf("failed to read archived content: %w", err)
	}

	if len(content) == 0 {
		a.log.Warn().Str("url", archiveURL).Msg("Archived content is empty")
		return nil, fmt.Errorf("archived content is empty")
	}

	a.log.Info().
		Str("url", targetURL).
		Str("archive_url", archiveURL).
		Int("content_length", len(content)).
		Msg("Successfully retrieved archived content")

	return content, nil
}
