package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"blob/server/internal/modules/identity"
	"blob/server/internal/modules/system"
	"blob/server/internal/platform/config"
	"blob/server/internal/platform/database"
	"blob/server/internal/platform/httpserver"
	"blob/server/internal/platform/observability"
	"blob/server/internal/transport/httpapi"

	"go.opentelemetry.io/otel"
)

func RunAPI(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	identityConfig, err := config.LoadIdentity()
	if err != nil {
		return fmt.Errorf("load identity config: %w", err)
	}
	shutdown, err := observability.Setup(ctx, cfg.OTEL.ServiceName, cfg.OTEL.Endpoint)
	if err != nil {
		return fmt.Errorf("setup telemetry: %w", err)
	}
	defer shutdownTelemetry(shutdown)
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	handler := httpapi.NewHandler(system.NewHealthChecker(pool), identity.NewSessions(pool), identityConfig, observability.HTTP(cfg.OTEL.ServiceName, os.Stdout))
	return httpserver.New(cfg.HTTP, httpserver.Security(handler)).Run(ctx)
}

func shutdownTelemetry(shutdown func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		otel.Handle(err)
	}
}
