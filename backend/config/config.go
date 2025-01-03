package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config holds all configuration for the application
type Config struct {
	ServerPort     string `mapstructure:"server_port"`
	KindleEmail    string `mapstructure:"kindle_email"`
	SenderEmail    string `mapstructure:"sender_email"`
	SenderPassword string `mapstructure:"sender_password"`
	LLMAPIKey      string `mapstructure:"llm_api_key"`
	Model          string `mapstructure:"model"`
}

// Load reads configuration from file or environment variables
func Load() (*Config, error) {
	// Set defaults
	viper.SetDefault("server_port", "8080")
	viper.SetDefault("model", "gemini-2.0-flash-exp")

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

	// Config file settings
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	// Get the executable path
	ex, err := os.Executable()
	if err != nil {
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
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		fmt.Printf("No config file found, checking paths: %v\n", configPaths)
	} else {
		fmt.Printf("Using config file: %s\n", viper.ConfigFileUsed())
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate required fields
	if config.KindleEmail == "" {
		return nil, fmt.Errorf("kindle_email is required")
	}
	if config.SenderEmail == "" {
		return nil, fmt.Errorf("sender_email is required")
	}
	if config.SenderPassword == "" {
		return nil, fmt.Errorf("sender_password is required")
	}
	if config.LLMAPIKey == "" {
		return nil, fmt.Errorf("llm_api_key is required")
	}

	return &config, nil
}
