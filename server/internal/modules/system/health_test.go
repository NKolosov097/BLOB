package system

import (
	"context"
	"errors"
	"testing"
	"time"
)

type pingerFunc func(context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error {
	return f(ctx)
}

func TestHealthCheckerLiveDoesNotRequireDatabase(t *testing.T) {
	if err := NewHealthChecker(nil).Live(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHealthCheckerReadyRejectsMissingDatabase(t *testing.T) {
	if err := NewHealthChecker(nil).Ready(context.Background()); err == nil {
		t.Fatal("expected unavailable database error")
	}
}

func TestHealthCheckerReadyPingsWithBoundedContext(t *testing.T) {
	wantErr := errors.New("database detail")
	checker := NewHealthChecker(pingerFunc(func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("ping context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > time.Second {
			t.Fatalf("unexpected ping deadline %s", remaining)
		}
		return wantErr
	}))

	if err := checker.Ready(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("got %v", err)
	}
}
