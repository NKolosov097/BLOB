package config

import (
	"errors"
	"testing"
	"time"
)

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	_, err := Load()
	if !errors.Is(err, ErrDatabaseURLRequired) {
		t.Fatalf("got %v", err)
	}
}

func TestLoadUsesBoundedHTTPDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Addr != ":8080" ||
		cfg.HTTP.ReadHeaderTimeout != 5*time.Second ||
		cfg.HTTP.ReadTimeout != 15*time.Second ||
		cfg.HTTP.WriteTimeout != 30*time.Second ||
		cfg.HTTP.IdleTimeout != 60*time.Second ||
		cfg.HTTP.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected HTTP defaults: %+v", cfg.HTTP)
	}
}

func TestLoadUsesEnvironmentValues(t *testing.T) {
	t.Setenv("DATABASE_URL", " postgres://example ")
	t.Setenv("APP_ENV", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_SERVICE_NAME", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Environment != "development" || cfg.HTTP.Addr != ":8080" || cfg.OTEL.Endpoint != "" || cfg.OTEL.ServiceName != "blob-api" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	t.Setenv("APP_ENV", "production")
	t.Setenv("HTTP_ADDR", "127.0.0.1:9000")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	t.Setenv("OTEL_SERVICE_NAME", "custom-api")

	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Environment != "production" || cfg.DatabaseURL != "postgres://example" || cfg.HTTP.Addr != "127.0.0.1:9000" {
		t.Fatalf("unexpected application config: %+v", cfg)
	}
	if cfg.OTEL.Endpoint != "http://collector:4318" || cfg.OTEL.ServiceName != "custom-api" {
		t.Fatalf("unexpected OTEL config: %+v", cfg.OTEL)
	}
}
