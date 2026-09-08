package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadAppliesSafeDefaults(t *testing.T) {
	values := map[string]string{
		"BEARER_TOKEN": "token",
		"DATABASE_URL": "postgres://db/attic",
		"AI_BASE_URL":  "https://provider.example/v1",
		"AI_API_KEY":   "key-secret",
	}
	cfg, err := LoadFrom(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Model != DefaultAIModel || cfg.AI.ReasoningEffort != "medium" || cfg.DefaultProfile != "a5" {
		t.Fatalf("defaults = %#v", cfg)
	}
	if cfg.AI.Timeout != 90*time.Second || cfg.AI.MaxRetries != 2 || cfg.AI.OmitReasoningEffort || cfg.AI.OmitResponseFormat {
		t.Fatalf("AI defaults = %#v", cfg.AI)
	}
	if cfg.MigrationsDir != "/app/migrations" || cfg.DBPoolMin != 1 || cfg.DBPoolMax != 5 {
		t.Fatalf("database defaults = %#v", cfg)
	}
}

func TestBrowserAndPDFLimitsAreParsedAndValidated(t *testing.T) {
	values := map[string]string{"BEARER_TOKEN": "token", "DATABASE_URL": "postgres://db/attic", "AI_BASE_URL": "https://provider.example/v1", "AI_API_KEY": "key", "BROWSER_RENDER_TIMEOUT": "25s", "BROWSER_MAX_DOM_BYTES": "12345", "PDF_MAX_BYTES": "54321", "PDF_TIMEOUT": "12s"}
	cfg, err := LoadFrom(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Browser.RenderTimeout != 25*time.Second || cfg.Browser.MaxDOMBytes != 12345 || cfg.PDF.MaxBytes != 54321 || cfg.PDF.Timeout != 12*time.Second {
		t.Fatalf("runtime config = %#v %#v", cfg.Browser, cfg.PDF)
	}
	values["BROWSER_SCREENSHOT_WIDTH"] = "5000"
	if _, err := LoadFrom(func(key string) string { return values[key] }); err == nil || !strings.Contains(err.Error(), "BROWSER_SCREENSHOT_DIMENSIONS") {
		t.Fatalf("invalid dimensions = %v", err)
	}
}

func TestAICompatibilityOptionsAreParsedAndBounded(t *testing.T) {
	values := map[string]string{
		"BEARER_TOKEN":            "token",
		"DATABASE_URL":            "postgres://db/attic",
		"AI_BASE_URL":             "https://provider.example/v1",
		"AI_API_KEY":              "key",
		"AI_REASONING_EFFORT":     "disabled",
		"AI_OMIT_RESPONSE_FORMAT": "true",
		"AI_TIMEOUT":              "45s",
		"AI_MAX_RETRIES":          "4",
	}
	cfg, err := LoadFrom(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AI.OmitReasoningEffort || cfg.AI.ReasoningEffort != "" || !cfg.AI.OmitResponseFormat || cfg.AI.Timeout != 45*time.Second || cfg.AI.MaxRetries != 4 {
		t.Fatalf("AI options = %#v", cfg.AI)
	}

	values["AI_TIMEOUT"] = "0s"
	if _, err := LoadFrom(func(key string) string { return values[key] }); err == nil || !strings.Contains(err.Error(), "AI_TIMEOUT") {
		t.Fatalf("invalid AI timeout = %v", err)
	}
	values["AI_TIMEOUT"] = "45s"
	values["AI_MAX_RETRIES"] = "11"
	if _, err := LoadFrom(func(key string) string { return values[key] }); err == nil || !strings.Contains(err.Error(), "AI_MAX_RETRIES") {
		t.Fatalf("invalid AI retries = %v", err)
	}
}

func TestValidationNamesFieldsWithoutSecrets(t *testing.T) {
	values := map[string]string{
		"BEARER_TOKEN": "super-secret-token",
		"DATABASE_URL": "postgres://db/attic",
		"AI_BASE_URL":  "not a URL",
		"AI_API_KEY":   "provider-secret",
	}
	_, err := LoadFrom(func(key string) string { return values[key] })
	if err == nil {
		t.Fatal("expected validation error")
	}
	message := err.Error()
	if !strings.Contains(message, "AI_BASE_URL") || strings.Contains(message, "super-secret") || strings.Contains(message, "provider-secret") {
		t.Fatalf("unsafe validation message = %q", message)
	}
}

func TestUnknownPDFProfileIsRejected(t *testing.T) {
	values := map[string]string{
		"BEARER_TOKEN": "token",
		"DATABASE_URL": "postgres://db/attic",
		"AI_BASE_URL":  "https://provider.example/v1",
		"AI_API_KEY":   "key",
		"PDF_PROFILE":  "a4",
	}
	_, err := LoadFrom(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "PDF_PROFILE") {
		t.Fatalf("unknown profile validation = %v", err)
	}
}

func TestDatabasePoolBoundsAreParsedAndValidated(t *testing.T) {
	values := map[string]string{
		"BEARER_TOKEN": "token",
		"DATABASE_URL": "postgres://db/attic",
		"AI_BASE_URL":  "https://provider.example/v1",
		"AI_API_KEY":   "key",
		"DB_POOL_MIN":  "2",
		"DB_POOL_MAX":  "8",
	}
	cfg, err := LoadFrom(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBPoolMin != 2 || cfg.DBPoolMax != 8 {
		t.Fatalf("pool bounds = %d/%d", cfg.DBPoolMin, cfg.DBPoolMax)
	}
	values["DB_POOL_MIN"] = "9"
	_, err = LoadFrom(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "DB_POOL") {
		t.Fatalf("invalid pool bounds = %v", err)
	}
}

func TestSubscriptionProviderDoesNotRequireAPIKey(t *testing.T) {
	values := map[string]string{"BEARER_TOKEN": "owner", "DATABASE_URL": "postgres://db/attic", "AI_PROVIDER": "chatgpt"}
	cfg, err := LoadFrom(func(key string) string { return values[key] })
	if err != nil || cfg.AI.AuthFile != "/data/auth/chatgpt.json" {
		t.Fatalf("subscription config: %v", err)
	}
	values["AI_PROVIDER"] = "typo"
	if _, err = LoadFrom(func(key string) string { return values[key] }); err == nil {
		t.Fatal("unknown provider accepted")
	}
	values["AI_PROVIDER"] = "api"
	if _, err = LoadFrom(func(key string) string { return values[key] }); err == nil {
		t.Fatal("API credentials no longer required")
	}
}

func TestKindleScribeProfile(t *testing.T) {
	values := map[string]string{"BEARER_TOKEN": "token", "DATABASE_URL": "postgres://db/attic", "AI_PROVIDER": "chatgpt", "PDF_PROFILE": "kindle-scribe"}
	cfg, err := LoadFrom(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Profiles["kindle-scribe"]; !ok || cfg.DefaultProfile != "kindle-scribe" {
		t.Fatal("Scribe profile not activated")
	}
}
