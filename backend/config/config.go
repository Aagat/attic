package config

import (
	"github.com/spf13/viper"
)

// Config holds all configuration for the application
type Config struct {
	ServerPort     string `mapstructure:"server_port"`
	KindleEmail    string `mapstructure:"kindle_email"`
	SenderEmail    string `mapstructure:"sender_email"`
	SenderPassword string `mapstructure:"sender_password"`
	LLMAPIKey      string `mapstructure:"llm_api_key"`
}

// Load reads configuration from file or environment variables
func Load() (*Config, error) {
	viper.SetDefault("server_port", "8080")

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.AutomaticEnv()

	var config Config
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
