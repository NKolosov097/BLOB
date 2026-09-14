package app

import (
	"context"
	"fmt"

	"blob/server/internal/modules/system"
	"blob/server/internal/platform/config"
	"blob/server/internal/platform/database"
	"blob/server/internal/platform/httpserver"
	"blob/server/internal/transport/httpapi"
)

func RunAPI(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	handler := httpapi.NewHandler(system.NewHealthChecker(pool))
	return httpserver.New(cfg.HTTP, handler).Run(ctx)
}
