package httpserver

import (
	"context"
	"net/http"
	"testing"
	"time"

	"blob/server/internal/platform/config"
)

func TestNewAppliesHTTPConfig(t *testing.T) {
	cfg := config.HTTP{
		Addr:              "127.0.0.1:0",
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
		ShutdownTimeout:   5 * time.Second,
	}
	handler := http.NewServeMux()

	server := New(cfg, handler)

	if server.http.Addr != cfg.Addr || server.http.Handler != handler ||
		server.http.ReadHeaderTimeout != cfg.ReadHeaderTimeout ||
		server.http.ReadTimeout != cfg.ReadTimeout ||
		server.http.WriteTimeout != cfg.WriteTimeout ||
		server.http.IdleTimeout != cfg.IdleTimeout ||
		server.shutdownTimeout != cfg.ShutdownTimeout {
		t.Fatalf("server does not match config: %+v", server.http)
	}
}

func TestRunShutsDownCleanlyWhenContextIsCanceled(t *testing.T) {
	cfg := config.HTTP{
		Addr:              "127.0.0.1:0",
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
		ShutdownTimeout:   time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := New(cfg, http.NewServeMux()).Run(ctx); err != nil {
		t.Fatal(err)
	}
}
