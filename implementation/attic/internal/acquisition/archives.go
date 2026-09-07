package acquisition

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

// Archives discovers existing snapshots. It never requests creation of a snapshot.
// Each call owns its source list, so exhausting one job cannot affect another.
type Archives struct{}

func (Archives) Candidates(ctx context.Context, original string) []string {
	u, err := url.Parse(original)
	if err != nil || ValidateURL(u) != nil || archiveHost(u.Hostname()) {
		return nil
	}
	u.Fragment = ""
	original = u.String()
	candidates := []string{"https://archive.ph/newest/" + original, "https://archive.is/newest/" + original}
	fetcher := NewFetcherWithConfig(FetchConfig{Timeout: 15 * time.Second, MaxBytes: 64 << 10, MaxRedirects: 3})
	page, err := fetcher.FetchJSON(ctx, "https://archive.org/wayback/available?url="+url.QueryEscape(original))
	if err != nil {
		slog.Info("Wayback discovery unavailable")
		return candidates
	}
	if snapshot := waybackSnapshot(page.HTML, original); snapshot != "" {
		candidates = append(candidates, snapshot)
	} else {
		slog.Info("Wayback has no matching available snapshot")
	}
	return candidates
}

func waybackSnapshot(body []byte, original string) string {
	var response struct {
		Snapshots struct {
			Closest struct {
				Available bool
				URL       string
				Status    string
			} `json:"closest"`
		} `json:"archived_snapshots"`
	}
	if json.Unmarshal(body, &response) != nil {
		return ""
	}
	s := response.Snapshots.Closest
	u, err := url.Parse(s.URL)
	if err != nil || !s.Available || s.Status != "200" || u.Host != "web.archive.org" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/web/"), "/", 2)
	if !strings.HasPrefix(u.Path, "/web/") || len(parts) != 2 {
		return ""
	}
	target, err := url.Parse(parts[1])
	wanted, err2 := url.Parse(original)
	if err != nil || err2 != nil || target.Host != wanted.Host || target.Path != wanted.Path || u.RawQuery != wanted.RawQuery {
		return ""
	}
	u.Scheme = "https"
	return u.String()
}

func archiveHost(host string) bool {
	switch strings.ToLower(host) {
	case "archive.ph", "archive.is", "archive.today", "archive.md", "archive.fo", "archive.li", "archive.vn", "web.archive.org":
		return true
	}
	return false
}
