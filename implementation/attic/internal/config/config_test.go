package config

import (
	"strings"
	"testing"
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
	if cfg.AI.Model != DefaultAIModel || cfg.AI.ReasoningEffort != "medium" || cfg.DefaultProfile != "a5" || cfg.SMTP.Enabled {
		t.Fatalf("defaults = %#v", cfg)
	}
	if cfg.MigrationsDir != "/app/migrations" || cfg.DBPoolMin != 1 || cfg.DBPoolMax != 5 {
		t.Fatalf("database defaults = %#v", cfg)
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

func TestEnabledSMTPRequiresTLSAndConnectionFields(t *testing.T) {
	values := map[string]string{
		"BEARER_TOKEN":  "token",
		"DATABASE_URL":  "postgres://db/attic",
		"AI_BASE_URL":   "https://provider.example/v1",
		"AI_API_KEY":    "key",
		"SMTP_ENABLED":  "true",
		"SMTP_TLS_MODE": "plain",
		"SMTP_PORT":     "2525",
	}
	_, err := LoadFrom(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "SMTP_TLS_MODE") {
		t.Fatalf("SMTP validation = %v", err)
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
