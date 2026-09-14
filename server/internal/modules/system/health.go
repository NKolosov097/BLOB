package system

import (
	"context"
	"errors"
	"time"
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
	return h.db.Ping(ctx)
}
