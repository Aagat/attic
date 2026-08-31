// Package config parses deployment configuration without ever including
// secret values in validation errors.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultAIModel       = "gpt-5.6-luna"
	defaultMigrationsDir = "/app/migrations"
	defaultDBPoolMin     = 1
	defaultDBPoolMax     = 5
	knownPDFProfile      = "a5"
)

type Config struct {
	ListenAddress string
	PublicBaseURL string
	BearerToken   string
	DatabaseURL   string
	MigrationsDir string
	DBPoolMin     int
	DBPoolMax     int
	ArtifactRoot  string

	DefaultProfile string
	Profiles       map[string]struct{}

	AI   AIConfig
	SMTP SMTPConfig
}

type AIConfig struct {
	BaseURL         string
	APIKey          string
	Model           string
	ReasoningEffort string
}

type SMTPConfig struct {
	Enabled     bool
	Host        string
	Port        int
	TLSMode     string
	Username    string
	Password    string
	Sender      string
	Destination string
}

type ValidationError struct {
	Fields []string
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Fields) == 0 {
		return "invalid configuration"
	}
	return "invalid configuration: " + strings.Join(e.Fields, ", ")
}

func Load() (Config, error) {
	return LoadFrom(os.Getenv)
}

func LoadFrom(get func(string) string) (Config, error) {
	if get == nil {
		get = func(string) string { return "" }
	}
	config := Config{
		ListenAddress:  valueOr(get("LISTEN_ADDRESS"), ":8080"),
		PublicBaseURL:  valueOr(get("PUBLIC_BASE_URL"), "http://localhost:8080"),
		BearerToken:    get("BEARER_TOKEN"),
		DatabaseURL:    get("DATABASE_URL"),
		MigrationsDir:  valueOr(get("MIGRATIONS_DIR"), defaultMigrationsDir),
		DBPoolMin:      defaultDBPoolMin,
		DBPoolMax:      defaultDBPoolMax,
		ArtifactRoot:   valueOr(get("ARTIFACT_ROOT"), "/data/artifacts"),
		DefaultProfile: valueOr(get("PDF_PROFILE"), "a5"),
		Profiles:       map[string]struct{}{"a5": {}},
		AI: AIConfig{
			BaseURL:         get("AI_BASE_URL"),
			APIKey:          get("AI_API_KEY"),
			Model:           valueOr(get("AI_MODEL"), DefaultAIModel),
			ReasoningEffort: valueOr(get("AI_REASONING_EFFORT"), "medium"),
		},
		SMTP: SMTPConfig{
			Host:        get("SMTP_HOST"),
			Port:        587,
			TLSMode:     valueOr(get("SMTP_TLS_MODE"), "starttls"),
			Username:    get("SMTP_USERNAME"),
			Password:    get("SMTP_PASSWORD"),
			Sender:      get("SMTP_SENDER"),
			Destination: get("SMTP_DESTINATION"),
		},
	}
	if rawPort := strings.TrimSpace(get("SMTP_PORT")); rawPort != "" {
		port, parseErr := strconv.Atoi(rawPort)
		if parseErr != nil {
			return Config{}, &ValidationError{Fields: []string{"SMTP_PORT"}}
		}
		config.SMTP.Port = port
	}
	if rawMin := strings.TrimSpace(get("DB_POOL_MIN")); rawMin != "" {
		poolMin, parseErr := strconv.Atoi(rawMin)
		if parseErr != nil {
			return Config{}, &ValidationError{Fields: []string{"DB_POOL_MIN"}}
		}
		config.DBPoolMin = poolMin
	}
	if rawMax := strings.TrimSpace(get("DB_POOL_MAX")); rawMax != "" {
		poolMax, parseErr := strconv.Atoi(rawMax)
		if parseErr != nil {
			return Config{}, &ValidationError{Fields: []string{"DB_POOL_MAX"}}
		}
		config.DBPoolMax = poolMax
	}
	smtpEnabled, err := parseBool(get("SMTP_ENABLED"), false)
	if err != nil {
		return Config{}, &ValidationError{Fields: []string{"SMTP_ENABLED"}}
	}
	config.SMTP.Enabled = smtpEnabled
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	fields := make([]string, 0)
	require := func(name, value string) {
		if strings.TrimSpace(value) == "" {
			fields = append(fields, name)
		}
	}
	require("BEARER_TOKEN", c.BearerToken)
	require("DATABASE_URL", c.DatabaseURL)
	require("MIGRATIONS_DIR", c.MigrationsDir)
	require("AI_BASE_URL", c.AI.BaseURL)
	require("AI_API_KEY", c.AI.APIKey)
	require("ARTIFACT_ROOT", c.ArtifactRoot)
	if strings.TrimSpace(c.AI.BaseURL) != "" {
		parsed, err := url.Parse(c.AI.BaseURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			fields = append(fields, "AI_BASE_URL")
		}
	}
	if strings.TrimSpace(c.PublicBaseURL) != "" {
		parsed, err := url.Parse(c.PublicBaseURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			fields = append(fields, "PUBLIC_BASE_URL")
		}
	}
	if strings.TrimSpace(c.DefaultProfile) == "" || c.DefaultProfile != knownPDFProfile {
		fields = append(fields, "PDF_PROFILE")
	}
	if len(c.Profiles) == 0 {
		fields = append(fields, "PDF_PROFILE")
	}
	for profile := range c.Profiles {
		if profile != knownPDFProfile {
			fields = append(fields, "PDF_PROFILE")
		}
	}
	if c.DBPoolMin < 0 {
		fields = append(fields, "DB_POOL_MIN")
	}
	if c.DBPoolMax < 1 {
		fields = append(fields, "DB_POOL_MAX")
	}
	if c.DBPoolMin > c.DBPoolMax {
		fields = append(fields, "DB_POOL_MIN", "DB_POOL_MAX")
	}
	if c.SMTP.Enabled {
		require("SMTP_HOST", c.SMTP.Host)
		require("SMTP_SENDER", c.SMTP.Sender)
		require("SMTP_DESTINATION", c.SMTP.Destination)
		require("SMTP_PASSWORD", c.SMTP.Password)
		if c.SMTP.Port <= 0 {
			fields = append(fields, "SMTP_PORT")
		}
		if c.SMTP.TLSMode != "implicit_tls" && c.SMTP.TLSMode != "starttls" {
			fields = append(fields, "SMTP_TLS_MODE")
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: uniqueStrings(fields)}
	}
	return nil
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func parseBool(value string, fallback bool) (bool, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, err
	}
	return parsed, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// Secret values must never be formatted into errors.  This helper is useful
// to callers validating additional fields in future configuration groups.
func SafeFieldError(field string) error {
	if strings.TrimSpace(field) == "" {
		return errors.New("invalid configuration")
	}
	return fmt.Errorf("invalid configuration: %s", field)
}
