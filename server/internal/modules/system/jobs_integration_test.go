package system

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"blob/server/internal/db"
	"blob/server/internal/platform/database"
	"blob/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func jobDatabase(t *testing.T) (*pgxpool.Pool, *db.Queries) {
	t.Helper()
	pool, err := database.Open(t.Context(), testdb.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(t.Context(), "TRUNCATE jobs"); err != nil {
		t.Fatal(err)
	}
	return pool, db.New(pool)
}

func enqueue(t *testing.T, queries *db.Queries, kind string, id byte) db.Job {
	t.Helper()
	job, err := queries.EnqueueJob(t.Context(), db.EnqueueJobParams{
		ID: pgtype.UUID{Bytes: [16]byte{id}, Valid: true}, Kind: kind,
		Payload:     []byte(`{"private":"payload must not appear in last_error_code"}`),
		AvailableAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func assertJob(t *testing.T, pool *pgxpool.Pool, id pgtype.UUID, status string, attempts int, code string) {
	t.Helper()
	var gotStatus, gotCode string
	var gotAttempts int
	var unlocked bool
	if err := pool.QueryRow(t.Context(), `SELECT status, attempts, COALESCE(last_error_code, ''), locked_at IS NULL FROM jobs WHERE id=$1`, id).Scan(&gotStatus, &gotAttempts, &gotCode, &unlocked); err != nil {
		t.Fatal(err)
	}
	if gotStatus != status || gotAttempts != attempts || gotCode != code || !unlocked {
		t.Fatalf("job = (%s, %d, %q, unlocked=%t), want (%s, %d, %q, unlocked=true)", gotStatus, gotAttempts, gotCode, unlocked, status, attempts, code)
	}
}

func TestOnlyOneWorkerClaimsJob(t *testing.T) {
	pool, queries := jobDatabase(t)
	job := enqueue(t, queries, "system.noop", 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var executions atomic.Int32
	release := make(chan struct{})
	handler := func(ctx context.Context, payload json.RawMessage) error {
		executions.Add(1)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	type result struct {
		claimed bool
		err     error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		worker := &Jobs{queries: db.New(pool), handlers: map[string]Handler{"system.noop": handler}}
		go func() {
			<-start
			claimed, err := worker.RunOnce(ctx)
			results <- result{claimed, err}
		}()
	}
	close(start)
	select {
	case first := <-results:
		if first.err != nil || first.claimed {
			t.Fatalf("second claimant = %+v, want no work while first handler runs", first)
		}
	case <-ctx.Done():
		t.Fatal("workers did not skip the active job")
	}
	close(release)
	select {
	case second := <-results:
		if second.err != nil || !second.claimed {
			t.Fatalf("winning worker = %+v", second)
		}
	case <-ctx.Done():
		t.Fatal("winning worker did not finish")
	}
	if executions.Load() != 1 {
		t.Fatalf("handler executed %d times, want 1", executions.Load())
	}
	assertJob(t, pool, job.ID, "done", 1, "")
}

func TestHandlerFailureRetriesThenStops(t *testing.T) {
	pool, queries := jobDatabase(t)
	job := enqueue(t, queries, "system.noop", 1)
	worker := &Jobs{queries: queries, handlers: map[string]Handler{
		"system.noop": func(context.Context, json.RawMessage) error {
			return errors.New("private handler details must not be persisted")
		},
	}}
	for attempt := 1; attempt <= 5; attempt++ {
		claimed, err := worker.RunOnce(t.Context())
		if err != nil || !claimed {
			t.Fatalf("attempt %d: claimed=%t, err=%v", attempt, claimed, err)
		}
		if attempt == 5 {
			assertJob(t, pool, job.ID, "failed", 5, "handler_failed")
			break
		}
		assertJob(t, pool, job.ID, "pending", attempt, "handler_failed")
		var delay float64
		if err := pool.QueryRow(t.Context(), `SELECT EXTRACT(EPOCH FROM available_at - updated_at)::float8 FROM jobs WHERE id=$1`, job.ID).Scan(&delay); err != nil {
			t.Fatal(err)
		}
		if want := []float64{1, 4, 9, 16}[attempt-1]; delay != want {
			t.Fatalf("attempt %d: retry delay=%v, want %v seconds", attempt, delay, want)
		}
		if _, err := pool.Exec(t.Context(), "UPDATE jobs SET available_at=now() WHERE id=$1", job.ID); err != nil {
			t.Fatal(err)
		}
	}
	if claimed, err := worker.RunOnce(t.Context()); err != nil || claimed {
		t.Fatalf("terminal job claimed=%t, err=%v", claimed, err)
	}
}

func TestNoopCompletesAndStaleJobIsReclaimed(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "pending"
		if stale {
			name = "stale"
		}
		t.Run(name, func(t *testing.T) {
			pool, queries := jobDatabase(t)
			job := enqueue(t, queries, "system.noop", 1)
			wantAttempts := 1
			if stale {
				if _, err := queries.ClaimJob(t.Context()); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(t.Context(), "UPDATE jobs SET locked_at=now()-interval '6 minutes', last_error_code='handler_failed' WHERE id=$1", job.ID); err != nil {
					t.Fatal(err)
				}
				wantAttempts = 2
			}
			claimed, err := NewJobs(queries).RunOnce(t.Context())
			if err != nil || !claimed {
				t.Fatalf("claimed=%t, err=%v", claimed, err)
			}
			assertJob(t, pool, job.ID, "done", wantAttempts, "")
		})
	}
}

func TestExpiredFifthAttemptIsTerminal(t *testing.T) {
	pool, queries := jobDatabase(t)
	job := enqueue(t, queries, "system.noop", 1)
	if _, err := pool.Exec(t.Context(), `UPDATE jobs SET status='running', attempts=5, locked_at=now()-interval '6 minutes' WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err := NewJobs(queries).RunOnce(t.Context())
	if err != nil || claimed {
		t.Fatalf("exhausted lease claimed=%t, err=%v", claimed, err)
	}
	assertJob(t, pool, job.ID, "failed", 5, "lease_expired")
}

func TestUnknownJobKindUsesSafeCode(t *testing.T) {
	pool, queries := jobDatabase(t)
	job := enqueue(t, queries, "private unknown kind", 1)
	claimed, err := NewJobs(queries).RunOnce(t.Context())
	if err != nil || !claimed {
		t.Fatalf("claimed=%t, err=%v", claimed, err)
	}
	assertJob(t, pool, job.ID, "pending", 1, "unknown_job_kind")
}

func TestFailRejectsNonAllowlistedCode(t *testing.T) {
	pool, queries := jobDatabase(t)
	job := enqueue(t, queries, "system.noop", 1)
	if _, err := queries.ClaimJob(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := NewJobs(queries).fail(t.Context(), job, "private raw error"); err == nil {
		t.Fatal("non-allowlisted code accepted")
	}
	var status string
	var noError bool
	if err := pool.QueryRow(t.Context(), "SELECT status, last_error_code IS NULL FROM jobs WHERE id=$1", job.ID).Scan(&status, &noError); err != nil {
		t.Fatal(err)
	}
	if status != "running" || !noError {
		t.Fatalf("rejected failure changed job: status=%s, noError=%t", status, noError)
	}
}

func TestCancellationDoesNotClaimMoreWork(t *testing.T) {
	t.Run("before claim", func(t *testing.T) {
		pool, queries := jobDatabase(t)
		job := enqueue(t, queries, "system.noop", 1)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		worker := NewJobs(queries)
		if claimed, err := worker.RunOnce(ctx); claimed || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled claim=%t, err=%v", claimed, err)
		}
		if err := worker.Run(ctx); err != nil {
			t.Fatal(err)
		}
		assertJob(t, pool, job.ID, "pending", 0, "")
	})
	t.Run("during handler", func(t *testing.T) {
		pool, queries := jobDatabase(t)
		enqueue(t, queries, "system.noop", 1)
		second := enqueue(t, queries, "system.noop", 2)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		worker := NewJobs(queries)
		executions := 0
		worker.handlers["system.noop"] = func(context.Context, json.RawMessage) error {
			executions++
			cancel()
			return ctx.Err()
		}
		if err := worker.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if executions != 1 {
			t.Fatalf("handler executed %d times, want 1", executions)
		}
		assertJob(t, pool, second.ID, "pending", 0, "")
	})
	t.Run("while idle", func(t *testing.T) {
		_, queries := jobDatabase(t)
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		start := time.Now()
		if err := NewJobs(queries).Run(ctx); err != nil {
			t.Fatal(err)
		}
		if ctx.Err() == nil {
			t.Fatal("idle worker exited before cancellation")
		}
		if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
			t.Fatalf("idle cancellation took %s; waited for the poll interval", elapsed)
		}
	})
}

func TestWorkerReturnsDatabaseFailure(t *testing.T) {
	pool, queries := jobDatabase(t)
	pool.Close()
	if err := NewJobs(queries).Run(t.Context()); err == nil {
		t.Fatal("closed database did not stop the worker")
	}
}

func TestJobTraceStartsIndependentRootAndHidesSecrets(t *testing.T) {
	for _, kind := range []string{"system.noop", "kind-secret"} {
		t.Run(kind, func(t *testing.T) {
			_, queries := jobDatabase(t)
			enqueue(t, queries, kind, 1)
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(recorder))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
			parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled})
			ctx := trace.ContextWithSpanContext(t.Context(), parent)
			if claimed, err := NewJobs(queries).RunOnce(ctx); err != nil || !claimed {
				t.Fatalf("job execution claimed=%t err=%v", claimed, err)
			}
			spans := recorder.Ended()
			if len(spans) != 1 || spans[0].Name() != "job.run" {
				t.Fatalf("got %d spans, want one job.run", len(spans))
			}
			span := spans[0]
			if span.Parent().IsValid() || span.SpanContext().TraceID() == parent.TraceID() {
				t.Fatal("worker job inherited producer trace")
			}
			attributes := map[string]any{}
			for _, attr := range span.Attributes() {
				attributes[string(attr.Key)] = attr.Value.AsInterface()
			}
			wantKind := "system.noop"
			if kind != "system.noop" {
				wantKind = "unknown"
			}
			if len(attributes) != 3 || attributes["operation"] != "run" || attributes["job.kind"] != wantKind || attributes["success"] != (kind == "system.noop") {
				t.Fatalf("unexpected job span attributes: %v", attributes)
			}
			if len(span.Events()) != 0 || len(span.Links()) != 0 || span.Status().Description != "" {
				t.Fatal("unexpected job span details")
			}
			encoded, err := json.Marshal(attributes)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "payload") {
				t.Fatal("job span leaked data")
			}
		})
	}
}
