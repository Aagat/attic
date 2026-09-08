package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Config struct {
	URL, APIKey, Index string
	Timeout            time.Duration // Entire operation, including queued indexing tasks; default 30s.
	Client             *http.Client
}

type Meilisearch struct {
	base, key, index string
	timeout          time.Duration
	client           *http.Client
	setup            chan struct{}
	ready            atomic.Bool
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func NewMeilisearch(c Config) (*Meilisearch, error) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, ErrInvalid
	}
	if c.Index == "" {
		c.Index = "attic"
	}
	if !identifier.MatchString(c.Index) {
		return nil, ErrInvalid
	}
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	client := http.Client{}
	if c.Client != nil {
		client = *c.Client
	}
	// Never forward the index key to a redirect destination.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Meilisearch{base: strings.TrimRight(c.URL, "/"), key: c.APIKey, index: c.Index, timeout: c.Timeout, client: &client, setup: make(chan struct{}, 1)}, nil
}

type indexedDocument struct {
	Document
	SavedUnix int64 `json:"saved_unix"`
}

func (m *Meilisearch) Upsert(ctx context.Context, d Document) error {
	if !identifier.MatchString(d.ID) || d.SavedAt.IsZero() {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	if err := m.ensure(ctx); err != nil {
		return err
	}
	return m.mutate(ctx, http.MethodPut, m.path("/documents"), []indexedDocument{{Document: d, SavedUnix: d.SavedAt.Unix()}})
}

func (m *Meilisearch) Delete(ctx context.Context, id string) error {
	if !identifier.MatchString(id) {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	if err := m.ensure(ctx); err != nil {
		return err
	}
	return m.mutate(ctx, http.MethodDelete, m.path("/documents/"+id), nil)
}

func (m *Meilisearch) path(suffix string) string { return "/indexes/" + m.index + suffix }

func (m *Meilisearch) ensure(ctx context.Context) error {
	if m.ready.Load() {
		return nil
	}
	select {
	case m.setup <- struct{}{}:
		defer func() { <-m.setup }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if m.ready.Load() {
		return nil
	}
	status, err := m.request(ctx, http.MethodGet, m.path(""), nil, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		if err := m.mutate(ctx, http.MethodPost, "/indexes", map[string]any{"uid": m.index, "primaryKey": "id"}); err != nil {
			return err
		}
	} else if status != http.StatusOK {
		return unavailable(status)
	}
	settings := map[string]any{
		"searchableAttributes": []string{"title", "tags", "notes", "url", "domain", "text", "kind", "capture_status"},
		"filterableAttributes": []string{"tags", "domain", "saved_unix", "capture_status", "kind"},
		"pagination":           map[string]int{"maxTotalHits": 100000},
	}
	if err := m.mutate(ctx, http.MethodPatch, m.path("/settings"), settings); err != nil {
		return err
	}
	m.ready.Store(true)
	return nil
}

func (m *Meilisearch) mutate(ctx context.Context, method, path string, body any) error {
	var task struct {
		UID *int64 `json:"taskUid"`
	}
	status, err := m.request(ctx, method, path, body, &task)
	if err != nil {
		return err
	}
	if status != http.StatusAccepted || task.UID == nil {
		return unavailable(status)
	}
	for {
		var state struct {
			Status string `json:"status"`
		}
		status, err = m.request(ctx, http.MethodGet, "/tasks/"+strconv.FormatInt(*task.UID, 10), nil, &state)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return unavailable(status)
		}
		switch state.Status {
		case "succeeded":
			return nil
		case "failed", "canceled":
			m.ready.Store(false)
			return ErrUnavailable
		case "enqueued", "processing":
		default:
			return ErrUnavailable
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func quoted(s string) string { b, _ := json.Marshal(s); return string(b) }

func (m *Meilisearch) Search(ctx context.Context, q Query) (Result, error) {
	empty := Result{Hits: []Hit{}, TotalEstimated: true}
	if q.Limit == 0 {
		q.Limit = 20
	}
	if q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || q.Offset > 99999 || (q.From != nil && q.To != nil && q.From.After(*q.To)) {
		return empty, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	if err := m.ensure(ctx); err != nil {
		return empty, err
	}
	filters := []string{}
	for _, tag := range q.Tags {
		filters = append(filters, "tags = "+quoted(tag))
	}
	if q.Kind != "" {
		if q.Kind != "bookmark" && q.Kind != "pdf" {
			return Result{}, ErrInvalid
		}
		filters = append(filters, "kind = "+quoted(q.Kind))
	}
	if q.Domain != "" {
		filters = append(filters, "domain = "+quoted(q.Domain))
	}
	if q.CaptureStatus != "" {
		filters = append(filters, "capture_status = "+quoted(q.CaptureStatus))
	}
	if q.From != nil {
		filters = append(filters, "saved_unix >= "+strconv.FormatInt(q.From.Unix(), 10))
	}
	if q.To != nil {
		filters = append(filters, "saved_unix <= "+strconv.FormatInt(q.To.Unix(), 10))
	}
	body := map[string]any{"q": q.Text, "filter": filters, "limit": q.Limit, "offset": q.Offset,
		"attributesToRetrieve": []string{"id", "title", "url", "notes", "tags", "domain", "kind", "capture_status", "saved_at"},
		"attributesToCrop":     []string{"text:40", "notes:40", "title:40"}, "cropMarker": "…", "showMatchesPosition": true,
	}
	var response struct {
		Hits []struct {
			Document
			Matches   map[string]json.RawMessage          `json:"_matchesPosition"`
			Formatted struct{ Text, Notes, Title string } `json:"_formatted"`
		} `json:"hits"`
		Total int `json:"estimatedTotalHits"`
	}
	status, err := m.request(ctx, http.MethodPost, m.path("/search"), body, &response)
	if err != nil {
		return empty, err
	}
	if status != http.StatusOK {
		return empty, unavailable(status)
	}
	result := Result{Hits: make([]Hit, 0, len(response.Hits)), Total: response.Total, TotalEstimated: true}
	for _, hit := range response.Hits {
		snippet := ""
		fields := []struct{ name, value string }{
			{"text", hit.Formatted.Text}, {"notes", hit.Formatted.Notes}, {"title", hit.Formatted.Title},
			{"url", hit.URL}, {"tags", strings.Join(hit.Tags, ", ")}, {"domain", hit.Domain}, {"kind", hit.Kind}, {"capture_status", hit.CaptureStatus},
		}
		for _, field := range fields {
			if len(hit.Matches[field.name]) > 2 && field.value != "" {
				snippet = field.value
				break
			}
		}
		if snippet == "" {
			snippet = hit.Formatted.Text
		}
		if snippet == "" {
			snippet = hit.Formatted.Notes
		}
		if snippet == "" {
			snippet = hit.Formatted.Title
		}
		result.Hits = append(result.Hits, Hit{Document: hit.Document, Snippet: snippet})
	}
	return result, nil
}

func unavailable(status int) error { return fmt.Errorf("%w (HTTP %d)", ErrUnavailable, status) }

func (m *Meilisearch) request(ctx context.Context, method, path string, body, out any) (int, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, ErrInvalid
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.base+path, payload)
	if err != nil {
		return 0, ErrInvalid
	}
	req.Header.Set("Content-Type", "application/json")
	if m.key != "" {
		req.Header.Set("Authorization", "Bearer "+m.key)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, ErrUnavailable
	}
	defer resp.Body.Close()
	// A missing index returns 404, but a document upsert may also implicitly
	// recreate it without our settings; filtered search then returns 400. Any
	// backend error invalidates initialization so the next retry repairs settings.
	if resp.StatusCode >= http.StatusBadRequest {
		m.ready.Store(false)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && out != nil {
		// Bound memory and never include backend payloads, indexed text, or keys in errors.
		const maximum = 16 << 20
		data, err := io.ReadAll(io.LimitReader(resp.Body, maximum+1))
		if err != nil || len(data) > maximum || json.Unmarshal(data, out) != nil {
			return resp.StatusCode, ErrUnavailable
		}
	}
	return resp.StatusCode, nil
}
