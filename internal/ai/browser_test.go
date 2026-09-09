package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func browserTestInput() BrowserInput {
	return BrowserInput{URL: "https://example.com/article", ScreenshotDataURL: "data:image/png;base64,aGVsbG8=", Text: strings.Repeat("文", 10000) + "PRIVATE_TAIL", Step: 2}
}

func TestBrowserStepUsesBoundedScreenshotAndSeparateInstruction(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Messages []struct {
				Role    string
				Content json.RawMessage
			}
			ResponseFormat map[string]string `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		var instruction string
		if len(body.Messages) != 2 {
			t.Fatal("missing messages")
		}
		json.Unmarshal(body.Messages[0].Content, &instruction)
		var parts []contentPart
		if err := json.Unmarshal(body.Messages[1].Content, &parts); err != nil {
			t.Fatal(err)
		}
		if instruction != browserInstruction || len(parts) != 2 || parts[1].ImageURL.URL != browserTestInput().ScreenshotDataURL || !utf8.ValidString(parts[0].Text) || strings.Contains(parts[0].Text, "PRIVATE_TAIL") || body.ResponseFormat["type"] != "json_object" {
			t.Fatal("incorrect browser input mapping")
		}
		writeCompletion(t, w, `{"action":"click","x":0,"y":767}`)
	}))
	defer server.Close()
	client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
	result, err := client.BrowserStep(context.Background(), browserTestInput())
	if err != nil || result.Action != "click" || result.Y != 767 || calls != 1 {
		t.Fatalf("%+v %v calls=%d", result, err, calls)
	}
	if client.systemInstruction != defaultSystemInstruction {
		t.Fatal("mutated shared instruction")
	}
}

func TestBrowserStepRejectsUnsafeModelActionsWithoutRepair(t *testing.T) {
	for _, output := range []string{
		`{}`, `{"action":"evaluate","text":"fetch('/private')"}`, `{"action":"click","x":1024,"y":10}`, `{"action":"click","x":3,"y":-1}`, `{"action":"click","y":1}`, `{"action":"click","x":null,"y":1}`, `{"action":"click","x":1e999,"y":0}`,
		`{"action":"done","url":"https://evil.example"}`, `{"action":"wait","text":"secret"}`, `{"action":"scroll","delta_y":0}`, `{"action":"scroll","delta_y":769}`, `{"action":"type","text":"line\nbreak"}`, `{"action":"type","text":"\u001b"}`, `{"action":"type","text":"\u202Eabc"}`, `{"action":"type","text":"` + strings.Repeat("x", 501) + `"}`, `{"action":"done"} {"action":"click","x":1,"y":1}`,
	} {
		t.Run(output, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; writeCompletion(t, w, output) }))
			defer server.Close()
			client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
			_, err := client.BrowserStep(context.Background(), browserTestInput())
			if CodeOf(err) != CodeAIInvalidResponse || calls != 1 {
				t.Fatalf("unsafe output accepted/retried: %v, calls=%d", err, calls)
			}
		})
	}
}

func TestBrowserActionValidInteractions(t *testing.T) {
	for _, output := range []string{`{"action":"type","text":"ABC 123"}`, `{"action":"scroll","delta_y":-768}`, `{"action":"scroll","delta_y":768}`, `{"action":"wait"}`, `{"action":"handoff"}`, `{"action":"done"}`} {
		if _, err := parseBrowserAction(output); err != nil {
			t.Fatalf("%s: %v", output, err)
		}
	}
}

func TestBrowserStepSubscriptionUsesDedicatedInstruction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Instructions string
			Input        []struct {
				Content []struct {
					Type     string
					ImageURL string `json:"image_url"`
				}
			}
			Store  bool
			Stream bool
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Instructions != browserInstruction || body.Store || !body.Stream || len(body.Input) != 1 || len(body.Input[0].Content) != 2 || body.Input[0].Content[1].Type != "input_image" || body.Input[0].Content[1].ImageURL != browserTestInput().ScreenshotDataURL {
			t.Fatal("incorrect subscription mapping")
		}
		fmt.Fprint(w, subscriptionEvent(`{"action":"handoff"}`))
	}))
	defer server.Close()
	client, err := NewSubscriptionClient(Config{Timeout: time.Second}, testSubscriptionToken{})
	if err != nil {
		t.Fatal(err)
	}
	client.subscription.endpoint = server.URL
	result, err := client.BrowserStep(context.Background(), browserTestInput())
	if err != nil || result.Action != "handoff" {
		t.Fatalf("%+v %v", result, err)
	}
	if client.systemInstruction != defaultSystemInstruction {
		t.Fatal("mutated subscription instruction")
	}
}

func TestBrowserStepBoundsInputAndResponse(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, strings.Repeat("x", (64<<10)+1)) }))
	defer server.Close()
	client := mustClient(t, Config{BaseURL: server.URL + "/v1", APIKey: "secret", AllowInsecureHTTP: true})
	input := browserTestInput()
	input.URL = "https://user:secret@example.com/article"
	if _, err := client.BrowserStep(context.Background(), input); CodeOf(err) != CodeInvalidInput {
		t.Fatal(err)
	}
	input = browserTestInput()
	input.ScreenshotDataURL = strings.Repeat("x", int(client.maxInputBytes)+1)
	if _, err := client.BrowserStep(context.Background(), input); CodeOf(err) != CodeInputTooLarge {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("invalid input reached provider")
	}
	if _, err := client.BrowserStep(context.Background(), browserTestInput()); CodeOf(err) != CodeAIResponseTooLarge || calls != 1 {
		t.Fatalf("response bound: %v calls=%d", err, calls)
	}
}
