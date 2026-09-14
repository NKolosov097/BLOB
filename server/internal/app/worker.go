package app

import (
	"context"
	"fmt"

	"blob/server/internal/db"
	"blob/server/internal/modules/system"
	"blob/server/internal/platform/config"
	"blob/server/internal/platform/database"
	"blob/server/internal/platform/observability"
)

func RunWorker(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
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
	return system.NewJobs(db.New(pool)).Run(ctx)
}
