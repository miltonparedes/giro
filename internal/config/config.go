// Package config handles application configuration via environment variables.
package config

import "os"

// Config holds the application configuration.
type Config struct {
	Host     string
	Port     string
	LogLevel string
	APIKey   string //nolint:gosec // not a hardcoded credential
}

// Addr returns the host:port address string.
func (c Config) Addr() string {
	return c.Host + ":" + c.Port
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		Host:     getEnv("HOST", "0.0.0.0"),
		Port:     getEnv("PORT", "8080"),
		LogLevel: getEnv("LOG_LEVEL", "info"),
		APIKey:   getEnv("API_KEY", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
