package extractor

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"
)

type archiveExtractor struct {
	client *http.Client
}

func newArchiveExtractor() *archiveExtractor {
	return &archiveExtractor{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type waybackResponse struct {
	ArchivedSnapshots struct {
		Closest struct {
			Available bool   `json:"available"`
			URL       string `json:"url"`
		} `json:"closest"`
	} `json:"archived_snapshots"`
}

func (a *archiveExtractor) extract(ctx context.Context, targetURL string) ([]byte, error) {
	// Query Wayback Machine API
	apiURL := fmt.Sprintf("https://archive.org/wayback/available?url=%s", url.QueryEscape(targetURL))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("archive API returned status %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var wayback waybackResponse
	if err := json.Unmarshal(body, &wayback); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !wayback.ArchivedSnapshots.Closest.Available {
		return nil, fmt.Errorf("no archived version available")
	}

	// Get archived content
	archiveReq, err := http.NewRequestWithContext(ctx, "GET", wayback.ArchivedSnapshots.Closest.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create archive request: %w", err)
	}

	archiveResp, err := a.client.Do(archiveReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get archived content: %w", err)
	}
	defer archiveResp.Body.Close()

	if archiveResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("archive returned status %d", archiveResp.StatusCode)
	}

	content, err := ioutil.ReadAll(archiveResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read archived content: %w", err)
	}

	if len(content) == 0 {
		return nil, fmt.Errorf("archived content is empty")
	}

	return content, nil
}
