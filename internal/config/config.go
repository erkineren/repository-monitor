package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramBotToken string
	DatabaseURL      string
	RenotifyInterval int
	PollInterval     int
	PollingTimeout   int
	Debug            bool
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf("error loading .env file: %v", err)
	}

	renotifyInterval, err := strconv.Atoi(getEnvWithDefault("RENOTIFY_INTERVAL", "3600"))
	if err != nil {
		return nil, fmt.Errorf("invalid RENOTIFY_INTERVAL: %v", err)
	}

	pollInterval, err := strconv.Atoi(getEnvWithDefault("POLL_INTERVAL", "60"))
	if err != nil {
		return nil, fmt.Errorf("invalid POLL_INTERVAL: %v", err)
	}

	return &Config{
		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		RenotifyInterval: renotifyInterval,
		PollInterval:     pollInterval,
		PollingTimeout:   60,    // Default Telegram polling timeout
		Debug:            false, // Debug mode disabled by default
	}, nil
}

func getEnvWithDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
