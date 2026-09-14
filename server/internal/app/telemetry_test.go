package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"blob/server/internal/platform/observability"
)

func TestTelemetryShutdownHasFreshBoundedContext(t *testing.T) {
	called := false
	shutdownTelemetry(func(ctx context.Context) error {
		called = true
		deadline, ok := ctx.Deadline()
		if ctx.Err() != nil || !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second {
			t.Fatal("telemetry shutdown context is canceled or unbounded")
		}
		return nil
	})
	if !called {
		t.Fatal("telemetry shutdown not called")
	}
}

func TestTelemetryShutdownReportsOnlySafeFailure(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	previousStderr := os.Stderr
	os.Stderr = writer
	t.Cleanup(func() { os.Stderr = previousStderr; _ = writer.Close() })
	shutdown, err := observability.Setup(t.Context(), "blob-worker", "")
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())
	shutdownTelemetry(func(context.Context) error { return errors.New("shutdown-secret") })
	_ = writer.Close()
	os.Stderr = previousStderr
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "shutdown-secret") {
		t.Fatal("shutdown error leaked")
	}
	var event map[string]any
	if err := json.Unmarshal(output, &event); err != nil {
		t.Fatal("shutdown failure was not reported")
	}
	if len(event) != 4 || event["event"] != "telemetry_failure" || event["service"] != "blob-worker" {
		t.Fatalf("unexpected shutdown diagnostic: %v", event)
	}
}
