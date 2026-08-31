package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAnalyzePostsCompatibleRequest(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		writeCompletion(t, w, `{"classification":"article","decision":"accept_candidate","title":"A title","completeness":0.9,"confidence":0.8,"decision_reason":"complete"}`)
	}))
	defer server.Close()

	client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
	approved, attempts, err := client.Analyze(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if approved.Title != "A title" || approved.ContentHTML != validRequest().CandidateHTML {
		t.Fatalf("approved = %#v", approved)
	}
	if len(attempts) != 1 || attempts[0].Status != AttemptSucceeded {
		t.Fatalf("attempts = %#v", attempts)
	}
	if attempts[0].Latency <= 0 {
		t.Fatalf("attempt latency = %s, want nonzero", attempts[0].Latency)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if got["model"] != DefaultModel {
		t.Errorf("model = %#v, want %q", got["model"], DefaultModel)
	}
	if got["stream"] != false {
		t.Errorf("stream = %#v, want false", got["stream"])
	}
	if got["reasoning_effort"] != DefaultReasoningEffort {
		t.Errorf("reasoning_effort = %#v, want %q", got["reasoning_effort"], DefaultReasoningEffort)
	}
	if format, ok := got["response_format"].(map[string]interface{}); !ok || format["type"] != "json_object" {
		t.Errorf("response_format = %#v", got["response_format"])
	}

	messages, ok := got["messages"].([]interface{})
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v", got["messages"])
	}
	user, ok := messages[1].(map[string]interface{})
	if !ok {
		t.Fatalf("user message = %#v", messages[1])
	}
	parts, ok := user["content"].([]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("user content = %#v", user["content"])
	}
	textPart := parts[0].(map[string]interface{})
	if textPart["type"] != "text" || !strings.Contains(textPart["text"].(string), "https://example.com/article") {
		t.Errorf("text part = %#v", textPart)
	}
	imagePart := parts[1].(map[string]interface{})
	imageURL := imagePart["image_url"].(map[string]interface{})
	if imagePart["type"] != "image_url" || imageURL["url"] != validRequest().ScreenshotDataURL {
		t.Errorf("image part = %#v", imagePart)
	}
}

func TestAnalyzeUsesConfiguredModelAndCanOmitReasoningEffort(t *testing.T) {
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeCompletion(t, w, validOutput("replace_candidate"))
	}))
	defer server.Close()

	client := mustClient(t, Config{
		BaseURL:             server.URL + "/v1/",
		APIKey:              "secret",
		Model:               "custom-model",
		ReasoningEffort:     "high",
		OmitReasoningEffort: true,
		OmitResponseFormat:  true,
		AllowInsecureHTTP:   true,
	})
	approved, _, err := client.Analyze(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if approved.Decision != "replace_candidate" {
		t.Fatalf("decision = %q", approved.Decision)
	}
	if got["model"] != "custom-model" {
		t.Errorf("model = %#v", got["model"])
	}
	if _, present := got["reasoning_effort"]; present {
		t.Errorf("reasoning_effort present in %#v", got)
	}
	if _, present := got["response_format"]; present {
		t.Errorf("response_format present in %#v", got)
	}
}

func TestAnalyzeParsesMetadataUsageAndProviderRequestID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "provider-request-1")
		writeCompletionWithUsage(t, w, validOutput("replace_candidate"), 17, 23)
	}))
	defer server.Close()

	client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", PromptVersion: "prompt-7", AllowInsecureHTTP: true})
	approved, attempts, err := client.Analyze(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if approved.ContentHTML != "<article><p>cleaned</p></article>" || approved.Language != "en" {
		t.Fatalf("approved = %#v", approved)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempts = %#v", attempts)
	}
	attempt := attempts[0]
	if attempt.ProviderRequestID != "provider-request-1" || attempt.InputTokens != 17 || attempt.OutputTokens != 23 || attempt.PromptVersion != "prompt-7" {
		t.Fatalf("attempt = %#v", attempt)
	}
}

func TestAnalyzeRepairsMalformedLogicalOutputExactlyOnce(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		messages := body["messages"].([]interface{})
		if call == 1 {
			if len(messages) != 2 {
				t.Errorf("first messages = %#v", messages)
			}
			writeCompletion(t, w, "not-json")
			return
		}
		if len(messages) != 4 {
			t.Errorf("repair messages = %#v", messages)
		}
		if messages[2].(map[string]interface{})["role"] != "assistant" {
			t.Errorf("repair assistant message = %#v", messages[2])
		}
		repairUser := messages[3].(map[string]interface{})
		repairParts := repairUser["content"].([]interface{})
		if len(repairParts) != 2 || repairParts[0].(map[string]interface{})["type"] != "text" || repairParts[1].(map[string]interface{})["type"] != "image_url" {
			t.Errorf("repair user content = %#v", repairUser["content"])
		}
		writeCompletion(t, w, validOutput("replace_candidate"))
	}))
	defer server.Close()

	client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
	approved, attempts, err := client.Analyze(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if approved.Decision != "replace_candidate" || calls.Load() != 2 {
		t.Fatalf("approved = %#v, calls = %d", approved, calls.Load())
	}
	if len(attempts) != 2 || attempts[0].Status != AttemptMalformed || attempts[1].Status != AttemptSucceeded {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestAnalyzeRepairsMalformedCompletionEnvelope(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, "not-json-envelope-secret")
			return
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode repair request: %v", err)
		}
		messages := body["messages"].([]interface{})
		if len(messages) != 4 {
			t.Errorf("repair messages = %#v", messages)
		}
		assistant := messages[2].(map[string]interface{})
		if assistant["content"] != noAssistantOutputMarker {
			t.Errorf("repair marker = %#v", assistant["content"])
		}
		writeCompletion(t, w, validOutput("replace_candidate"))
	}))
	defer server.Close()

	client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
	approved, attempts, err := client.Analyze(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if approved.Decision != "replace_candidate" || calls.Load() != 2 {
		t.Fatalf("approved = %#v, calls = %d", approved, calls.Load())
	}
	if len(attempts) != 2 || attempts[0].Status != AttemptMalformed || attempts[1].Status != AttemptSucceeded {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestAnalyzeRepairsMalformedCompletionVariants(t *testing.T) {
	cases := []struct {
		name  string
		first string
	}{
		{name: "no choices", first: `{"id":"bad-choices","choices":[]}`},
		{name: "empty assistant content", first: `{"id":"empty-content","choices":[{"message":{"role":"assistant","content":""}}]}`},
		{name: "non-assistant content", first: `{"id":"wrong-role","choices":[{"message":{"role":"tool","content":"not-assistant"}}]}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, test.first)
					return
				}
				writeCompletion(t, w, validOutput("replace_candidate"))
			}))
			defer server.Close()

			client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
			approved, attempts, err := client.Analyze(context.Background(), validRequest())
			if err != nil || approved.Decision != "replace_candidate" || calls.Load() != 2 {
				t.Fatalf("approved = %#v, calls = %d, error = %v", approved, calls.Load(), err)
			}
			if len(attempts) != 2 || attempts[0].Status != AttemptMalformed || attempts[1].Status != AttemptSucceeded {
				t.Fatalf("attempts = %#v", attempts)
			}
		})
	}
}

func TestAnalyzeRequiresModelTitleWithoutHintFallback(t *testing.T) {
	var calls atomic.Int32
	missingTitle := strings.Replace(validOutput("replace_candidate"), `"title":"A title",`, "", 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeCompletion(t, w, missingTitle)
	}))
	defer server.Close()

	client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
	approved, attempts, err := client.Analyze(context.Background(), validRequest())
	if !errors.Is(err, ErrAIInvalidResponse) {
		t.Fatalf("error = %v, want invalid response", err)
	}
	if approved != (Approved{}) {
		t.Fatalf("approved = %#v, want zero result", approved)
	}
	if calls.Load() != 2 || len(attempts) != 2 || attempts[0].Status != AttemptMalformed || attempts[1].Status != AttemptMalformed {
		t.Fatalf("calls = %d, attempts = %#v", calls.Load(), attempts)
	}
}

func TestAnalyzeRequiresShortDecisionReason(t *testing.T) {
	var calls atomic.Int32
	missingReason := strings.Replace(validOutput("replace_candidate"), `"decision_reason":"usable",`, "", 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeCompletion(t, w, missingReason)
	}))
	defer server.Close()

	client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
	_, attempts, err := client.Analyze(context.Background(), validRequest())
	if !errors.Is(err, ErrAIInvalidResponse) {
		t.Fatalf("error = %v, want invalid response", err)
	}
	if calls.Load() != 2 || len(attempts) != 2 || attempts[0].Status != AttemptMalformed || attempts[1].Status != AttemptMalformed {
		t.Fatalf("calls = %d, attempts = %#v", calls.Load(), attempts)
	}
}

func TestAnalyzeDoesNotAttemptSecondRepair(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeCompletion(t, w, "still-not-json")
	}))
	defer server.Close()

	client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
	_, attempts, err := client.Analyze(context.Background(), validRequest())
	if !errors.Is(err, ErrAIInvalidResponse) {
		t.Fatalf("error = %v, want ai_invalid_response", err)
	}
	if calls.Load() != 2 || len(attempts) != 2 {
		t.Fatalf("calls = %d, attempts = %#v", calls.Load(), attempts)
	}
	if attempts[1].Status != AttemptMalformed || attempts[1].ErrorCode != CodeAIInvalidResponse {
		t.Fatalf("second attempt = %#v", attempts[1])
	}
}

func TestAnalyzeResponseBodyLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, strings.Repeat("x", 256))
	}))
	defer server.Close()

	client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", MaxResponseBytes: 64, AllowInsecureHTTP: true})
	_, attempts, err := client.Analyze(context.Background(), validRequest())
	if !errors.Is(err, ErrAIResponseTooLarge) {
		t.Fatalf("error = %v, want response-too-large", err)
	}
	if len(attempts) != 1 || attempts[0].ErrorCode != CodeAIResponseTooLarge {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestAnalyzeTimeoutIsTypedAndSafe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer server.Close()

	client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", Timeout: 25 * time.Millisecond, AllowInsecureHTTP: true})
	_, attempts, err := client.Analyze(context.Background(), validRequest())
	if !errors.Is(err, ErrAITimeout) {
		t.Fatalf("error = %v, want timeout", err)
	}
	if strings.Contains(err.Error(), server.URL) || strings.Contains(err.Error(), "Context") {
		t.Fatalf("unsafe timeout error = %v", err)
	}
	if len(attempts) != 1 || attempts[0].ErrorCode != CodeAITimeout {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestAnalyzeClassifiesHTTPStatusesWithoutRawBodies(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantCode  Code
		wantRetry bool
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"error":{"message":"secret unauthorized body"}}`, wantCode: CodeAIAuthFailed},
		{name: "forbidden", status: http.StatusForbidden, body: "forbidden marker", wantCode: CodeAIAuthFailed},
		{name: "missing model endpoint", status: http.StatusNotFound, body: "model marker", wantCode: CodeAIModelUnsupported},
		{name: "model error", status: http.StatusBadRequest, body: `{"error":{"code":"model_not_found","message":"private model detail"}}`, wantCode: CodeAIModelUnsupported},
		{name: "rate limit", status: http.StatusTooManyRequests, body: "rate marker", wantCode: CodeAIRateLimited, wantRetry: true},
		{name: "server", status: http.StatusBadGateway, body: "server marker", wantCode: CodeAIUnavailable, wantRetry: true},
		{name: "generic provider rejection", status: http.StatusBadRequest, body: "raw secret marker", wantCode: CodeAIProviderRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()

			client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
			_, attempts, err := client.Analyze(context.Background(), validRequest())
			var typed *Error
			if !errors.As(err, &typed) {
				t.Fatalf("error type = %T (%v)", err, err)
			}
			if typed.Code != test.wantCode || typed.Retryable != test.wantRetry {
				t.Fatalf("error = %#v, want code=%q retry=%v", typed, test.wantCode, test.wantRetry)
			}
			if strings.Contains(err.Error(), "marker") || strings.Contains(err.Error(), "private") {
				t.Fatalf("raw provider body leaked: %v", err)
			}
			if len(attempts) != 1 || attempts[0].ErrorCode != test.wantCode {
				t.Fatalf("attempts = %#v", attempts)
			}
		})
	}
}

func TestAnalyzeDecisionCategoriesDoNotRepair(t *testing.T) {
	cases := []struct {
		classification string
		wantCode       Code
	}{
		{classification: "paywall", wantCode: CodePaywallDetected},
		{classification: "access_denied", wantCode: CodeAccessDenied},
		{classification: "interactive", wantCode: CodeUnsupportedContent},
		{classification: "non_article", wantCode: CodeUnsupportedContent},
	}
	for _, test := range cases {
		t.Run(test.classification, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				writeCompletion(t, w, fmt.Sprintf(`{"classification":%q,"decision":"reject"}`, test.classification))
			}))
			defer server.Close()

			client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
			_, attempts, err := client.Analyze(context.Background(), validRequest())
			if CodeOf(err) != test.wantCode || calls.Load() != 1 || len(attempts) != 1 || attempts[0].Status != AttemptRejected {
				t.Fatalf("error = %v, calls = %d, attempts = %#v", err, calls.Load(), attempts)
			}
		})
	}
}

func TestNewClientRequiresV1BaseURLAndKey(t *testing.T) {
	cases := []Config{
		{BaseURL: "https://example.test", APIKey: "key"},
		{BaseURL: "https://example.test/v2", APIKey: "key"},
		{BaseURL: "https://example.test/v1", APIKey: ""},
	}
	for _, config := range cases {
		if _, err := NewClient(config); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("config %#v: error = %v, want invalid config", config, err)
		}
	}
}

func TestNewClientRequiresHTTPSUnlessLoopbackOptIn(t *testing.T) {
	if _, err := NewClient(Config{BaseURL: "http://127.0.0.1/v1", APIKey: "key"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("default loopback HTTP error = %v, want invalid config", err)
	}
	if _, err := NewClient(Config{BaseURL: "http://127.0.0.1/v1", APIKey: "key", AllowInsecureHTTP: true}); err != nil {
		t.Fatalf("loopback HTTP opt-in error = %v", err)
	}
	if _, err := NewClient(Config{BaseURL: "http://localhost/v1", APIKey: "key", AllowInsecureHTTP: true}); err != nil {
		t.Fatalf("localhost HTTP opt-in error = %v", err)
	}
	if _, err := NewClient(Config{BaseURL: "http://203.0.113.10/v1", APIKey: "key", AllowInsecureHTTP: true}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("non-loopback HTTP error = %v, want invalid config", err)
	}
	if _, err := NewClient(Config{BaseURL: "http://example.test/v1", APIKey: "key"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("non-loopback HTTP default error = %v, want invalid config", err)
	}
	if _, err := NewClient(Config{BaseURL: "https://example.test/v1", APIKey: "key"}); err != nil {
		t.Fatalf("HTTPS config error = %v", err)
	}
}

func TestAnalyzeRejectsNonImageScreenshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("provider should not be called")
	}))
	defer server.Close()

	client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
	request := validRequest()
	request.ScreenshotDataURL = "https://example.com/image.png"
	_, attempts, err := client.Analyze(context.Background(), request)
	if !errors.Is(err, ErrInvalidInput) || len(attempts) != 0 {
		t.Fatalf("error = %v, attempts = %#v", err, attempts)
	}
}

func TestAnalyzeRejectsInvalidImageBase64(t *testing.T) {
	client := mustClient(t, Config{BaseURL: "https://example.test/v1", APIKey: "secret"})
	request := validRequest()
	request.ScreenshotDataURL = "data:image/png;base64,definitely-not-base64%%%"
	_, attempts, err := client.Analyze(context.Background(), request)
	if !errors.Is(err, ErrInvalidInput) || len(attempts) != 0 {
		t.Fatalf("error = %v, attempts = %#v", err, attempts)
	}
}

func mustClient(t *testing.T, config Config) *Client {
	t.Helper()
	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func validRequest() AnalyzeRequest {
	return AnalyzeRequest{
		SourceURL:         "https://example.com/article",
		CandidateText:     "Candidate article text",
		CandidateHTML:     "<article><p>candidate</p></article>",
		Title:             "Candidate title",
		Author:            "Candidate author",
		SiteName:          "Example",
		PublicationDate:   "2026-08-31",
		Description:       "Description",
		Language:          "en",
		ScreenshotDataURL: "data:image/png;base64,AAAA",
	}
}

func validOutput(decision string) string {
	content := ""
	if decision == "replace_candidate" {
		content = `,"cleaned_html":"<article><p>cleaned</p></article>"`
	}
	return `{"classification":"article","decision":` + fmt.Sprintf("%q", decision) + `,"title":"A title","author":"An author","site_name":"A site","publication_date":"2026-08-30","description":"A description","language":"en","completeness":0.95,"confidence":0.91,"decision_reason":"usable"` + content + `}`
}

func writeCompletion(t *testing.T, w http.ResponseWriter, assistantContent string) {
	t.Helper()
	writeCompletionWithUsage(t, w, assistantContent, 0, 0)
}

func writeCompletionWithUsage(t *testing.T, w http.ResponseWriter, assistantContent string, inputTokens, outputTokens int) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"id": "completion-id",
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": assistantContent,
				},
			},
		},
	}
	if inputTokens != 0 || outputTokens != 0 {
		response["usage"] = map[string]int{"prompt_tokens": inputTokens, "completion_tokens": outputTokens}
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		t.Errorf("encode completion: %v", err)
	}
}
