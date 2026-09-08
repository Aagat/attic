// Package config parses deployment configuration without ever including
// secret values in validation errors.
package config

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
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

	AI      AIConfig
	Browser BrowserConfig
	PDF     PDFConfig
}

type BrowserConfig struct {
	Executable                                                              string
	NavigationTimeout, RenderTimeout                                        time.Duration
	Concurrency, MaxRedirects, MaxDOMBytes, MaxDOMNodes, MaxScreenshotBytes int
	MaxTransferredBytes                                                     int64
	ScreenshotWidth, ScreenshotHeight                                       int64
}

type PDFConfig struct {
	MaxBytes                         int
	Timeout                          time.Duration
	MarginMM, BodyFontPT, LineHeight float64
}

type AIConfig struct {
	Provider            string
	AuthFile            string
	BaseURL             string
	APIKey              string
	Model               string
	ReasoningEffort     string
	OmitReasoningEffort bool
	OmitResponseFormat  bool
	Timeout             time.Duration
	MaxRetries          int
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
	reasoningEffort := strings.TrimSpace(get("AI_REASONING_EFFORT"))
	omitReasoningEffort := reasoningEffort == "off" || reasoningEffort == "none" || reasoningEffort == "disabled"
	if reasoningEffort == "" {
		reasoningEffort = "medium"
	}
	if omitReasoningEffort {
		reasoningEffort = ""
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
		Profiles:       map[string]struct{}{valueOr(get("PDF_PROFILE"), "a5"): {}},
		AI: AIConfig{
			Provider:            valueOr(get("AI_PROVIDER"), "api"),
			AuthFile:            valueOr(get("CHATGPT_AUTH_FILE"), "/data/auth/chatgpt.json"),
			BaseURL:             get("AI_BASE_URL"),
			APIKey:              get("AI_API_KEY"),
			Model:               valueOr(get("AI_MODEL"), DefaultAIModel),
			ReasoningEffort:     reasoningEffort,
			OmitReasoningEffort: omitReasoningEffort,
			Timeout:             90 * time.Second,
			MaxRetries:          2,
		},
		Browser: BrowserConfig{Executable: valueOr(get("BROWSER_EXECUTABLE"), "/usr/bin/chromium"), NavigationTimeout: 30 * time.Second, RenderTimeout: 45 * time.Second, Concurrency: 1, MaxRedirects: 5, MaxDOMBytes: 10_000_000, MaxDOMNodes: 100_000, MaxScreenshotBytes: 5_000_000, MaxTransferredBytes: 20_000_000, ScreenshotWidth: 1280, ScreenshotHeight: 1600},
		PDF:     PDFConfig{MaxBytes: 25_000_000, Timeout: 45 * time.Second, MarginMM: 12, BodyFontPT: 11, LineHeight: 1.25},
	}
	parseDuration := func(key string, target *time.Duration) error {
		raw := strings.TrimSpace(get(key))
		if raw == "" {
			return nil
		}
		value, err := time.ParseDuration(raw)
		if err != nil {
			return &ValidationError{Fields: []string{key}}
		}
		*target = value
		return nil
	}
	parseInt := func(key string, target *int) error {
		raw := strings.TrimSpace(get(key))
		if raw == "" {
			return nil
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return &ValidationError{Fields: []string{key}}
		}
		*target = value
		return nil
	}
	parseInt64 := func(key string, target *int64) error {
		raw := strings.TrimSpace(get(key))
		if raw == "" {
			return nil
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return &ValidationError{Fields: []string{key}}
		}
		*target = value
		return nil
	}
	parseFloat := func(key string, target *float64) error {
		raw := strings.TrimSpace(get(key))
		if raw == "" {
			return nil
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return &ValidationError{Fields: []string{key}}
		}
		*target = value
		return nil
	}
	for _, item := range []struct {
		key    string
		target *time.Duration
	}{{"BROWSER_NAVIGATION_TIMEOUT", &config.Browser.NavigationTimeout}, {"BROWSER_RENDER_TIMEOUT", &config.Browser.RenderTimeout}, {"PDF_TIMEOUT", &config.PDF.Timeout}} {
		if err := parseDuration(item.key, item.target); err != nil {
			return Config{}, err
		}
	}
	for _, item := range []struct {
		key    string
		target *int
	}{{"BROWSER_CONCURRENCY", &config.Browser.Concurrency}, {"BROWSER_MAX_REDIRECTS", &config.Browser.MaxRedirects}, {"BROWSER_MAX_DOM_BYTES", &config.Browser.MaxDOMBytes}, {"BROWSER_MAX_DOM_NODES", &config.Browser.MaxDOMNodes}, {"BROWSER_MAX_SCREENSHOT_BYTES", &config.Browser.MaxScreenshotBytes}, {"PDF_MAX_BYTES", &config.PDF.MaxBytes}} {
		if err := parseInt(item.key, item.target); err != nil {
			return Config{}, err
		}
	}
	for _, item := range []struct {
		key    string
		target *int64
	}{{"BROWSER_MAX_RESPONSE_BYTES", &config.Browser.MaxTransferredBytes}, {"BROWSER_SCREENSHOT_WIDTH", &config.Browser.ScreenshotWidth}, {"BROWSER_SCREENSHOT_HEIGHT", &config.Browser.ScreenshotHeight}} {
		if err := parseInt64(item.key, item.target); err != nil {
			return Config{}, err
		}
	}
	for _, item := range []struct {
		key    string
		target *float64
	}{{"PDF_MARGIN_MM", &config.PDF.MarginMM}, {"PDF_BODY_FONT_PT", &config.PDF.BodyFontPT}, {"PDF_LINE_HEIGHT", &config.PDF.LineHeight}} {
		if err := parseFloat(item.key, item.target); err != nil {
			return Config{}, err
		}
	}
	if rawTimeout := strings.TrimSpace(get("AI_TIMEOUT")); rawTimeout != "" {
		timeout, parseErr := time.ParseDuration(rawTimeout)
		if parseErr != nil {
			return Config{}, &ValidationError{Fields: []string{"AI_TIMEOUT"}}
		}
		config.AI.Timeout = timeout
	}
	if rawRetries := strings.TrimSpace(get("AI_MAX_RETRIES")); rawRetries != "" {
		retries, parseErr := strconv.Atoi(rawRetries)
		if parseErr != nil {
			return Config{}, &ValidationError{Fields: []string{"AI_MAX_RETRIES"}}
		}
		config.AI.MaxRetries = retries
	}
	omitResponseFormat, err := parseBool(get("AI_OMIT_RESPONSE_FORMAT"), false)
	if err != nil {
		return Config{}, &ValidationError{Fields: []string{"AI_OMIT_RESPONSE_FORMAT"}}
	}
	config.AI.OmitResponseFormat = omitResponseFormat
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
	switch c.AI.Provider {
	case "api", "":
		require("AI_BASE_URL", c.AI.BaseURL)
		require("AI_API_KEY", c.AI.APIKey)
	case "chatgpt":
		require("CHATGPT_AUTH_FILE", c.AI.AuthFile)
	default:
		fields = append(fields, "AI_PROVIDER")
	}
	require("ARTIFACT_ROOT", c.ArtifactRoot)
	if c.AI.Provider != "chatgpt" && strings.TrimSpace(c.AI.BaseURL) != "" {
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
	if strings.TrimSpace(c.DefaultProfile) == "" || (c.DefaultProfile != knownPDFProfile && c.DefaultProfile != "kindle-scribe") {
		fields = append(fields, "PDF_PROFILE")
	}
	if len(c.Profiles) == 0 {
		fields = append(fields, "PDF_PROFILE")
	}
	for profile := range c.Profiles {
		if profile != knownPDFProfile && profile != "kindle-scribe" {
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
	if c.AI.Timeout <= 0 || c.AI.Timeout > 10*time.Minute {
		fields = append(fields, "AI_TIMEOUT")
	}
	if c.AI.MaxRetries < 0 || c.AI.MaxRetries > 10 {
		fields = append(fields, "AI_MAX_RETRIES")
	}
	if strings.TrimSpace(c.Browser.Executable) == "" {
		fields = append(fields, "BROWSER_EXECUTABLE")
	}
	if c.Browser.NavigationTimeout <= 0 || c.Browser.RenderTimeout <= 0 {
		fields = append(fields, "BROWSER_NAVIGATION_TIMEOUT", "BROWSER_RENDER_TIMEOUT")
	}
	if c.Browser.Concurrency != 1 {
		fields = append(fields, "BROWSER_CONCURRENCY")
	}
	if c.Browser.MaxRedirects < 1 || c.Browser.MaxRedirects > 20 || c.Browser.MaxDOMBytes < 1 || c.Browser.MaxDOMNodes < 1 || c.Browser.MaxScreenshotBytes < 1 || c.Browser.MaxTransferredBytes < 1 {
		fields = append(fields, "BROWSER_LIMITS")
	}
	if c.Browser.ScreenshotWidth < 1 || c.Browser.ScreenshotWidth > 4096 || c.Browser.ScreenshotHeight < 1 || c.Browser.ScreenshotHeight > 4096 {
		fields = append(fields, "BROWSER_SCREENSHOT_DIMENSIONS")
	}
	if c.PDF.MaxBytes < 1 || c.PDF.Timeout <= 0 || c.PDF.MarginMM <= 0 || c.PDF.MarginMM > 50 || c.PDF.BodyFontPT < 6 || c.PDF.BodyFontPT > 30 || c.PDF.LineHeight < 1 || c.PDF.LineHeight > 3 {
		fields = append(fields, "PDF_LIMITS")
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
