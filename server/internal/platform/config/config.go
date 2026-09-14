package config

import (
	"errors"
	"os"
	"strings"
	"time"
)

var ErrDatabaseURLRequired = errors.New("DATABASE_URL is required")

type Config struct {
	Environment string
	DatabaseURL string
	HTTP        HTTP
	OTEL        OTEL
}

type HTTP struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

type OTEL struct {
	Endpoint    string
	ServiceName string
}

func Load() (Config, error) {
	databaseURL, _ := os.LookupEnv("DATABASE_URL")
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return Config{}, ErrDatabaseURLRequired
	}
	return Config{
		Environment: envOr("APP_ENV", "development"),
		DatabaseURL: databaseURL,
		HTTP: HTTP{
			Addr:              envOr("HTTP_ADDR", ":8080"),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			ShutdownTimeout:   10 * time.Second,
		},
		OTEL: OTEL{
			Endpoint:    envOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
			ServiceName: envOr("OTEL_SERVICE_NAME", "blob-api"),
		},
	}, nil
}

func envOr(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
