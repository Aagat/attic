package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testSubscriptionToken struct{}

func (testSubscriptionToken) Token(context.Context) (string, string, error) {
	return "private-token", "account", nil
}

func subscriptionEvent(text string) string {
	data, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "usage": map[string]int{"input_tokens": 123, "output_tokens": 45}, "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": text}}}}}})
	return "event: response.completed\ndata: " + string(data) + "\n\n"
}

func TestSubscriptionAnalyzesImageAndRepairsThroughResponses(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get("ChatGPT-Account-Id") != "account" {
			t.Error("missing subscription routing/auth")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("invalid JSON")
		}
		if body["stream"] != true || body["store"] != false || body["model"] != DefaultModel || body["messages"] != nil || body["instructions"] == nil {
			t.Error("wrong Responses encoding")
		}
		inputs := body["input"].([]any)
		parts := inputs[0].(map[string]any)["content"].([]any)
		image := parts[1].(map[string]any)
		if image["type"] != "input_image" || image["image_url"] != validRequest().ScreenshotDataURL {
			t.Error("image missing")
		}
		if calls == 1 {
			fmt.Fprint(w, subscriptionEvent("invalid-json"))
			return
		}
		if len(inputs) != 3 {
			t.Error("repair missing previous output and instructions")
		}
		w.Header().Set("X-Request-ID", "request-safe")
		fmt.Fprint(w, subscriptionEvent(validOutput("accept_candidate")))
	}))
	defer server.Close()
	c, err := NewSubscriptionClient(Config{}, testSubscriptionToken{})
	if err != nil {
		t.Fatal(err)
	}
	c.subscription.endpoint = server.URL + "/responses"
	approved, attempts, err := c.Analyze(context.Background(), validRequest())
	if err != nil || approved.Decision != "accept_candidate" || len(attempts) != 2 {
		t.Fatalf("result: %v, %#v", err, attempts)
	}
	if attempts[0].Status != AttemptMalformed || attempts[1].Status != AttemptSucceeded || attempts[1].InputTokens != 123 || !attempts[1].UsageReported {
		t.Fatalf("attempts: %#v", attempts)
	}
}

func TestSubscriptionDoesNotApprovePartialOrOversizedStream(t *testing.T) {
	for _, tc := range []struct {
		name, stream string
		limit        int64
		code         Code
	}{
		{"delta only", `data: {"type":"response.output_text.delta","delta":"{}"}` + "\n\ndata: [DONE]\n\n", 4096, CodeAIUnavailable},
		{"failed", `data: {"type":"response.failed"}` + "\n\n", 4096, CodeAIUnavailable},
		{"oversized", "data: " + strings.Repeat("x", 500), 64, CodeAIResponseTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := readResponsesStream(strings.NewReader(tc.stream), tc.limit)
			if CodeOf(err) != tc.code {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSubscriptionDoesNotFollowRedirectWithCredentials(t *testing.T) {
	reached := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	c, _ := NewSubscriptionClient(Config{}, testSubscriptionToken{})
	c.subscription.endpoint = origin.URL
	_, _, err := c.Analyze(context.Background(), validRequest())
	if reached || CodeOf(err) != CodeAIProviderRejected {
		t.Fatalf("redirect followed or wrong error: %v", err)
	}
}

func TestSubscriptionStreamUsesDoneItemsWhenCompletionOmitsOutput(t *testing.T) {
	output := validOutput("accept_candidate")
	item, _ := json.Marshal(map[string]any{"type": "response.output_item.done", "output_index": 1, "item": map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": output}}}})
	stream := "data: " + string(item) + "\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":8}}}\n\n"
	got, usage, err := readResponsesStream(strings.NewReader(stream), 4096)
	if err != nil || got != output || usage == nil || usage.Input != 5 {
		t.Fatalf("completed item not assembled: output_length=%d error=%v", len(got), err)
	}
	if _, _, err := readResponsesStream(strings.NewReader("data: "+string(item)+"\n\n"), 4096); CodeOf(err) != CodeAIUnavailable {
		t.Fatalf("partial response accepted: %v", err)
	}
}
