package system

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type Pinger interface {
	Ping(context.Context) error
}

type HealthChecker struct {
	db Pinger
}

func NewHealthChecker(db Pinger) HealthChecker {
	return HealthChecker{db: db}
}

func (h HealthChecker) Live(context.Context) error {
	return nil
}

func (h HealthChecker) Ready(ctx context.Context) error {
	if h.db == nil {
		return errors.New("database unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ctx, span := otel.Tracer("blob/system").Start(ctx, "postgres.ping")
	defer func() { span.End() }()
	err := h.db.Ping(ctx)
	span.SetAttributes(attribute.String("operation", "ping"), attribute.Bool("success", err == nil))
	return err
}
