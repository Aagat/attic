package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aagat/attic/backend/logger"
	"github.com/spf13/viper"
)

var (
	ErrMissingKindleEmail    = errors.New("kindle_email is required when email is enabled")
	ErrMissingSenderEmail    = errors.New("sender_email is required when email is enabled")
	ErrMissingSenderPassword = errors.New("sender_password is required when email is enabled")
	ErrMissingLLMAPIKey      = errors.New("llm_api_key is required")
)

// ArchiveConfig represents configuration for an archive source
type ArchiveConfig struct {
	Name      string `mapstructure:"name"`
	URLFormat string `mapstructure:"url_format"` // Format string where %s will be replaced with the target URL
	Priority  int    `mapstructure:"priority"`
}

// Config represents the application configuration
type Config struct {
	StoragePath    string          `mapstructure:"storage_path"`
	ServerPort     string          `mapstructure:"server_port"`
	LLMAPIKey      string          `mapstructure:"llm_api_key"`
	Model          string          `mapstructure:"model"`
	EmailEnabled   bool            `mapstructure:"email_enabled"`
	KindleEmail    string          `mapstructure:"kindle_email"`
	SenderEmail    string          `mapstructure:"sender_email"`
	SenderPassword string          `mapstructure:"sender_password"`
	Archives       []ArchiveConfig `mapstructure:"archives"`
}

// Load reads configuration from file or environment variables
func Load() (*Config, error) {
	log := logger.WithComponent("config")

	// Set defaults
	viper.SetDefault("server_port", "8080")
	viper.SetDefault("model", "gemini-2.0-flash-exp")
	viper.SetDefault("email_enabled", false)

	// Set default storage path to ./data
	defaultStoragePath := filepath.Join(".", "data")
	viper.SetDefault("storage_path", defaultStoragePath)

	// Set environment variable prefix
	viper.SetEnvPrefix("ATTIC")
	viper.AutomaticEnv()

	// Map environment variables
	viper.BindEnv("server_port", "ATTIC_SERVER_PORT")
	viper.BindEnv("kindle_email", "ATTIC_KINDLE_EMAIL")
	viper.BindEnv("sender_email", "ATTIC_SENDER_EMAIL")
	viper.BindEnv("sender_password", "ATTIC_SENDER_PASSWORD")
	viper.BindEnv("llm_api_key", "ATTIC_LLM_API_KEY")
	viper.BindEnv("model", "ATTIC_MODEL")
	viper.BindEnv("storage_path", "ATTIC_STORAGE_PATH")
	viper.BindEnv("email_enabled", "ATTIC_EMAIL_ENABLED")

	// Config file settings
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	// Get the executable path
	ex, err := os.Executable()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get executable path")
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}
	exPath := filepath.Dir(ex)

	// Add config paths
	configPaths := []string{
		".",
		"config",
		"backend/config",
		exPath,
		filepath.Join(exPath, "config"),
	}

	// Add user and system config paths
	home, err := os.UserHomeDir()
	if err == nil {
		configPaths = append(configPaths,
			filepath.Join(home, ".config/attic"),
			"/etc/attic",
		)
	}

	// Add all config paths to viper
	for _, path := range configPaths {
		viper.AddConfigPath(path)
	}

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			log.Error().Err(err).Msg("Failed to read config file")
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		log.Warn().Strs("paths", configPaths).Msg("No config file found, using defaults")
	} else {
		log.Info().Str("file", viper.ConfigFileUsed()).Msg("Configuration loaded")
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal config")
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate required fields only if email is enabled
	if config.EmailEnabled {
		if config.KindleEmail == "" {
			log.Error().Msg("Missing required field: kindle_email")
			return nil, ErrMissingKindleEmail
		}
		if config.SenderEmail == "" {
			log.Error().Msg("Missing required field: sender_email")
			return nil, ErrMissingSenderEmail
		}
		if config.SenderPassword == "" {
			log.Error().Msg("Missing required field: sender_password")
			return nil, ErrMissingSenderPassword
		}
	}

	// LLM API key is always required
	if config.LLMAPIKey == "" {
		log.Error().Msg("Missing required field: llm_api_key")
		return nil, ErrMissingLLMAPIKey
	}

	// Create storage directory if it doesn't exist
	if err := os.MkdirAll(config.StoragePath, 0755); err != nil {
		log.Error().Err(err).Str("path", config.StoragePath).Msg("Failed to create storage directory")
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	// Log final configuration state
	log.Info().
		Str("port", config.ServerPort).
		Str("storage", config.StoragePath).
		Bool("email_enabled", config.EmailEnabled).
		Msg("Configuration initialized")

	if config.EmailEnabled {
		log.Info().
			Str("kindle_email", config.KindleEmail).
			Str("sender_email", config.SenderEmail).
			Msg("Email delivery configured")
	}

	return &config, nil
}
