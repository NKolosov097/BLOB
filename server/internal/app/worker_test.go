package app

import (
	"errors"
	"testing"

	"blob/server/internal/platform/config"
)

func TestRunWorkerRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := RunWorker(t.Context()); !errors.Is(err, config.ErrDatabaseURLRequired) {
		t.Fatalf("RunWorker error=%v, want required database URL", err)
	}
}
