package acquisition

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Archives discovers existing snapshots. It never requests creation of a snapshot.
// Each call owns its source list, so exhausting one job cannot affect another.
// fetchJSON is the private test seam; production always uses the guarded Fetcher.
type Archives struct {
	fetchJSON func(context.Context, string) (Page, error)
}

func (a Archives) Candidates(ctx context.Context, original string) []string {
	u, err := url.Parse(original)
	if err != nil || ValidateURL(u) != nil || ctx.Err() != nil {
		return nil
	}
	u.Fragment = ""
	submitted := u.String()
	var candidates []string
	if archiveHost(u.Hostname()) {
		recovered := archiveOriginal(u)
		if recovered == "" {
			return nil
		}
		u, _ = url.Parse(recovered)
		candidates = append(candidates, recovered)
	}
	original = u.String()
	// Discovery shares one budget, leaving the caller time to retrieve a snapshot.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	fetch := a.fetchJSON
	if fetch == nil {
		fetch = NewFetcherWithConfig(FetchConfig{Timeout: 10 * time.Second, MaxBytes: 64 << 10, MaxRedirects: 3}).FetchJSON
	}
	snapshot := ""
	page, err := fetch(ctx, "https://archive.org/wayback/available?url="+url.QueryEscape(original))
	if err == nil {
		snapshot = waybackSnapshot(page.HTML, original)
	}
	if snapshot == "" || snapshot == submitted {
		// Availability returns only one capture. CDX also recovers when that service
		// is unavailable or its closest result does not match the requested page.
		query := url.Values{"url": {original}, "matchType": {"exact"}, "output": {"json"}, "fl": {"timestamp,original,statuscode"}, "filter": {"statuscode:200", "mimetype:text/html"}, "limit": {"-3"}, "fastLatest": {"true"}}
		if ctx.Err() == nil {
			page, err = fetch(ctx, "https://web.archive.org/cdx/search/cdx?"+query.Encode())
			if err == nil {
				snapshot = cdxSnapshot(page.HTML, original, submitted)
			}
		}
	}
	if snapshot != "" && snapshot != submitted {
		candidates = append(candidates, snapshot)
	} else {
		slog.Info("Wayback discovery has no usable matching snapshot")
	}
	// archive.today has no verified stable public JSON discovery API. These routes
	// remain best-effort HTML fallbacks, after API-discovered Wayback snapshots.
	return append(candidates, "https://archive.ph/newest/"+original, "https://archive.is/newest/"+original)
}

var replayStamp = regexp.MustCompile(`^[0-9]{1,14}(?:id_|im_|if_|js_|cs_)?$`)
var archiveTodayStamp = regexp.MustCompile(`^(?:newest|[0-9]{4}\.[0-9]{2}\.[0-9]{2}-[0-9]{6}|[0-9]{14})$`)

// archiveOriginal only unwraps replay paths containing an explicit source URL.
// Short archive.today IDs require page content, and cannot safely be guessed.
func archiveOriginal(u *url.URL) string {
	path := u.EscapedPath()
	var embedded string
	if strings.EqualFold(u.Hostname(), "web.archive.org") {
		parts := strings.SplitN(strings.TrimPrefix(path, "/web/"), "/", 2)
		if !strings.HasPrefix(path, "/web/") || len(parts) != 2 || !replayStamp.MatchString(parts[0]) {
			return ""
		}
		embedded = parts[1]
	} else if archiveHost(u.Hostname()) {
		path = strings.TrimPrefix(path, "/")
		if strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://") {
			embedded = path
		} else {
			parts := strings.SplitN(path, "/", 2)
			if len(parts) != 2 || !archiveTodayStamp.MatchString(parts[0]) {
				return ""
			}
			embedded = parts[1]
		}
	}
	if embedded == "" {
		return ""
	}
	if u.RawQuery != "" {
		embedded += "?" + u.RawQuery
	}
	target, err := url.Parse(embedded)
	if err != nil || ValidateURL(target) != nil || archiveHost(target.Hostname()) {
		return ""
	}
	target.Fragment = ""
	return target.String()
}

func matchingSource(source, original string) bool {
	target, err := url.Parse(source)
	wanted, err2 := url.Parse(original)
	if err != nil || err2 != nil || ValidateURL(target) != nil {
		return false
	}
	// Wayback may normalize HTTP to HTTPS. Preserve path escaping and query
	// exactly: /a%2Fb is not the same resource as /a/b.
	return strings.EqualFold(target.Host, wanted.Host) && target.EscapedPath() == wanted.EscapedPath() && target.RawQuery == wanted.RawQuery
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
	if err != nil || !s.Available || s.Status != "200" || !strings.EqualFold(u.Host, "web.archive.org") || ValidateURL(u) != nil {
		return ""
	}
	if !matchingSource(archiveOriginal(u), original) {
		return ""
	}
	u.Scheme = "https"
	u.Fragment = ""
	return u.String()
}

func cdxSnapshot(body []byte, original, exclude string) string {
	var rows [][]string
	if json.Unmarshal(body, &rows) != nil || len(rows) < 2 || len(rows[0]) != 3 || strings.Join(rows[0], ",") != "timestamp,original,statuscode" {
		return ""
	}
	latest := ""
	result := ""
	for _, row := range rows[1:] {
		if len(row) != 3 || len(row[0]) != 14 || !replayStamp.MatchString(row[0]) || row[2] != "200" || !matchingSource(row[1], original) {
			continue
		}
		candidate := "https://web.archive.org/web/" + row[0] + "/" + row[1]
		if candidate != exclude && row[0] > latest {
			latest, result = row[0], candidate
		}
	}
	return result
}

func archiveHost(host string) bool {
	switch strings.ToLower(host) {
	case "archive.org", "archive.ph", "archive.is", "archive.today", "archive.md", "archive.fo", "archive.li", "archive.vn", "web.archive.org":
		return true
	}
	return false
}
