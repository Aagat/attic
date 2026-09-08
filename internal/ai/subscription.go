package ai

import (
	"attic/internal/subscription"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const subscriptionEndpoint = "https://chatgpt.com/backend-api/codex/responses"

// TokenSource supplies a current subscription token and its owning account.
// Credentials never enter attempt records or provider error messages.
type TokenSource interface {
	Token(context.Context) (string, string, error)
}

type subscriptionTransport struct {
	tokens   TokenSource
	endpoint string
}

// NewSubscriptionClient uses direct Codex Responses HTTP requests while sharing
// the article schema, repair budget, limits, and approval semantics of Client.
func NewSubscriptionClient(config Config, tokens TokenSource) (*Client, error) {
	if tokens == nil {
		return nil, &Error{Code: CodeInvalidConfig, Message: "ChatGPT credentials are required"}
	}
	// These fields belong exclusively to the API-key transport. No request is
	// sent to this base URL by the subscription client.
	config.BaseURL = "https://api.openai.com/v1"
	config.APIKey = "subscription"
	c, err := NewClient(config)
	if err != nil {
		return nil, err
	}
	c.subscription = &subscriptionTransport{tokens: tokens, endpoint: subscriptionEndpoint}
	client := *c.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.httpClient = &client
	return c, nil
}

func (c *Client) subscriptionCall(ctx context.Context, body []byte, number int) (content string, attempt Attempt, callErr error) {
	attempt = Attempt{Number: number, CreatedAt: time.Now().UTC(), Model: c.model, PromptVersion: c.promptVersion, Status: AttemptFailed}
	started := time.Now()
	defer func() {
		attempt.Latency = time.Since(started)
		if callErr != nil {
			attempt.ErrorCode = CodeOf(callErr)
		}
	}()
	payload, err := c.responsesBody(body)
	if err != nil {
		return "", attempt, err
	}
	token, account, err := c.subscription.tokens.Token(ctx)
	if errors.Is(err, subscription.ErrUnavailable) {
		return "", attempt, &Error{Code: CodeAIUnavailable, Message: "ChatGPT authentication service unavailable", Retryable: true}
	}
	if err != nil || token == "" || account == "" || strings.ContainsAny(token+account, "\r\n") {
		if ctx.Err() != nil {
			return "", attempt, transportError(ctx, ctx.Err())
		}
		return "", attempt, &Error{Code: CodeAIAuthFailed, Message: "ChatGPT authentication failed; run attic login-chatgpt"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.subscription.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", attempt, &Error{Code: CodeAIUnavailable, Message: "ChatGPT request could not be created"}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("ChatGPT-Account-Id", account)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "attic/1.0")
	req.Header.Set("originator", "attic")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return "", attempt, transportError(ctx, err)
	}
	defer response.Body.Close()
	attempt.ProviderRequestID = providerRequestID(response.Header)
	if response.StatusCode != http.StatusOK {
		data, _, _ := readBounded(response.Body, c.maxResponseBytes)
		return "", attempt, classifyHTTPStatus(response.StatusCode, data)
	}
	content, usage, err := readResponsesStream(response.Body, c.maxResponseBytes)
	if err != nil {
		if ctx.Err() != nil {
			err = transportError(ctx, ctx.Err())
		}
		return "", attempt, err
	}
	if usage != nil {
		attempt.InputTokens = usage.Input
		attempt.OutputTokens = usage.Output
		attempt.UsageReported = true
	}
	return content, attempt, nil
}

func (c *Client) responsesBody(body []byte) ([]byte, error) {
	var chat struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &chat) != nil {
		return nil, invalidResponseError()
	}
	input := []any{}
	for _, m := range chat.Messages {
		if m.Role == "system" {
			continue
		}
		parts := []any{}
		var text string
		if json.Unmarshal(m.Content, &text) == nil {
			kind := "input_text"
			if m.Role == "assistant" {
				kind = "output_text"
			}
			parts = append(parts, map[string]any{"type": kind, "text": text})
		} else {
			var source []contentPart
			if json.Unmarshal(m.Content, &source) != nil {
				return nil, invalidResponseError()
			}
			for _, p := range source {
				if p.Type == "text" {
					parts = append(parts, map[string]any{"type": "input_text", "text": p.Text})
				}
				if p.Type == "image_url" && p.ImageURL != nil {
					parts = append(parts, map[string]any{"type": "input_image", "image_url": p.ImageURL.URL})
				}
			}
		}
		input = append(input, map[string]any{"role": m.Role, "content": parts})
	}
	request := map[string]any{"model": c.model, "instructions": c.systemInstruction, "input": input, "store": false, "stream": true}
	if !c.omitReasoningEffort {
		request["reasoning"] = map[string]string{"effort": c.reasoningEffort}
	}
	// The shared prompt specifies JSON; local validation and one repair remain
	// authoritative. Do not assume the Codex backend accepts API response_format.
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, &Error{Code: CodeInvalidInput, Message: "ChatGPT request could not be encoded"}
	}
	if int64(len(encoded)) > c.maxInputBytes {
		return nil, &Error{Code: CodeInputTooLarge, Message: "AI request exceeded the configured limit"}
	}
	return encoded, nil
}

type responseItem struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type responseUsage struct {
	Input  int `json:"input_tokens"`
	Output int `json:"output_tokens"`
}

// Only a response.completed event authorizes output. Text deltas and [DONE]
// alone are insufficient: a disconnected or failed stream must not approve it.
func readResponsesStream(reader io.Reader, limit int64) (string, *responseUsage, error) {
	limited := &io.LimitedReader{R: reader, N: limit + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), int(limit+1))
	var data strings.Builder
	var final string
	var usage *responseUsage
	completed := false
	doneItems := make(map[int]responseItem)
	consume := func() error {
		payload := strings.TrimSpace(data.String())
		data.Reset()
		if payload == "" || payload == "[DONE]" {
			return nil
		}
		var event struct {
			Type        string       `json:"type"`
			OutputIndex int          `json:"output_index"`
			Item        responseItem `json:"item"`
			Response    struct {
				Status string         `json:"status"`
				Usage  *responseUsage `json:"usage"`
				Output []responseItem `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(payload), &event) != nil {
			return &malformedCompletion{}
		}
		switch event.Type {
		case "response.output_item.done":
			doneItems[event.OutputIndex] = event.Item
		case "response.completed":
			if completed || event.Response.Status != "completed" {
				return invalidResponseError()
			}
			items := event.Response.Output
			if len(items) == 0 {
				indices := make([]int, 0, len(doneItems))
				for index := range doneItems {
					indices = append(indices, index)
				}
				sort.Ints(indices)
				for _, index := range indices {
					items = append(items, doneItems[index])
				}
			}
			var output strings.Builder
			for _, item := range items {
				if item.Type != "message" || item.Role != "assistant" {
					continue
				}
				for _, part := range item.Content {
					if part.Type == "output_text" {
						output.WriteString(part.Text)
					}
				}
			}
			final = output.String()
			if strings.TrimSpace(final) == "" {
				return &malformedCompletion{}
			}
			usage = event.Response.Usage
			completed = true
		case "response.failed", "response.incomplete", "error":
			return &Error{Code: CodeAIUnavailable, Message: "ChatGPT response did not complete", Retryable: true}
		}
		return nil
	}
	for scanner.Scan() {
		if limited.N == 0 {
			return "", nil, &Error{Code: CodeAIResponseTooLarge, Message: "AI response exceeded the configured limit"}
		}
		line := scanner.Text()
		if line == "" {
			if err := consume(); err != nil {
				return "", nil, err
			}
			if completed {
				return final, usage, nil
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimPrefix(line, "data:"))
			data.WriteByte('\n')
		}
	}
	if limited.N == 0 {
		return "", nil, &Error{Code: CodeAIResponseTooLarge, Message: "AI response exceeded the configured limit"}
	}
	if scanner.Err() != nil {
		return "", nil, &Error{Code: CodeAIUnavailable, Message: "ChatGPT response stream interrupted", Retryable: true}
	}
	if err := consume(); err != nil {
		return "", nil, err
	}
	if !completed {
		return "", nil, &Error{Code: CodeAIUnavailable, Message: "ChatGPT response stream ended before completion", Retryable: true}
	}
	if strings.TrimSpace(final) == "" {
		return "", nil, &malformedCompletion{}
	}
	return final, usage, nil
}

var _ Analyzer = (*Client)(nil)
