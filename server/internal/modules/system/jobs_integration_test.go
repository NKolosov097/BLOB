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

	"github.com/google/uuid"
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
	var unlocked, unclaimed bool
	if err := pool.QueryRow(t.Context(), `SELECT status, attempts, COALESCE(last_error_code, ''), locked_at IS NULL, claim_token IS NULL FROM jobs WHERE id=$1`, id).Scan(&gotStatus, &gotAttempts, &gotCode, &unlocked, &unclaimed); err != nil {
		t.Fatal(err)
	}
	if gotStatus != status || gotAttempts != attempts || gotCode != code || !unlocked || !unclaimed {
		t.Fatalf("job = (%s, %d, %q, unlocked=%t, unclaimed=%t), want (%s, %d, %q, unlocked=true, unclaimed=true)", gotStatus, gotAttempts, gotCode, unlocked, unclaimed, status, attempts, code)
	}
}

// testClaim mints the fencing token a worker would generate for one claim.
func testClaim(t *testing.T) pgtype.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return pgtype.UUID{Bytes: id, Valid: true}
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
				if _, err := queries.ClaimJob(t.Context(), testClaim(t)); err != nil {
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
	if _, err := queries.ClaimJob(t.Context(), testClaim(t)); err != nil {
		t.Fatal(err)
	}
	if err := NewJobs(queries).fail(t.Context(), trace.SpanFromContext(t.Context()), job, testClaim(t), "private raw error"); err == nil {
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

// TestStaleClaimCannotFinishReclaimedJob covers the fencing contract: once a
// lease expires and another worker reclaims the job, the previous worker's
// completion and failure writes must change nothing.
func TestStaleClaimCannotFinishReclaimedJob(t *testing.T) {
	pool, queries := jobDatabase(t)
	job := enqueue(t, queries, "system.noop", 1)

	stale := testClaim(t)
	if _, err := queries.ClaimJob(t.Context(), stale); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE jobs SET locked_at=now()-interval '6 minutes' WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	live := testClaim(t)
	reclaimed, err := queries.ClaimJob(t.Context(), live)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.ID != job.ID || reclaimed.Attempts != 2 {
		t.Fatalf("reclaim = (%v, attempts=%d), want the same job on attempt 2", reclaimed.ID, reclaimed.Attempts)
	}

	for name, write := range map[string]func() (int64, error){
		"complete": func() (int64, error) {
			return queries.CompleteJob(t.Context(), db.CompleteJobParams{ID: job.ID, ClaimToken: stale})
		},
		"fail": func() (int64, error) {
			return queries.FailJob(t.Context(), db.FailJobParams{
				ID: job.ID, ClaimToken: stale,
				LastErrorCode: pgtype.Text{String: "handler_failed", Valid: true},
			})
		},
	} {
		rows, err := write()
		if err != nil || rows != 0 {
			t.Fatalf("stale %s: rows=%d, err=%v, want 0 rows and no error", name, rows, err)
		}
	}

	var status, code string
	var attempts int
	var claim pgtype.UUID
	if err := pool.QueryRow(t.Context(), `SELECT status, attempts, COALESCE(last_error_code, ''), claim_token FROM jobs WHERE id=$1`, job.ID).Scan(&status, &attempts, &code, &claim); err != nil {
		t.Fatal(err)
	}
	if status != "running" || attempts != 2 || code != "" || claim.Bytes != live.Bytes {
		t.Fatalf("after stale writes job = (%s, %d, %q, claim held by the live worker=%t)", status, attempts, code, claim.Bytes == live.Bytes)
	}

	rows, err := queries.CompleteJob(t.Context(), db.CompleteJobParams{ID: job.ID, ClaimToken: live})
	if err != nil || rows != 1 {
		t.Fatalf("live completion: rows=%d, err=%v, want 1 row", rows, err)
	}
	assertJob(t, pool, job.ID, "done", 2, "")
}

// TestLostClaimIsNotAnError covers the worker side of the same race: a run
// whose claim was taken over reports no error and leaves the live outcome.
func TestLostClaimIsNotAnError(t *testing.T) {
	pool, queries := jobDatabase(t)
	job := enqueue(t, queries, "system.noop", 1)
	worker := &Jobs{queries: queries, handlers: map[string]Handler{
		"system.noop": func(context.Context, json.RawMessage) error {
			// Another worker takes the expired lease while this handler runs.
			if _, err := pool.Exec(context.Background(), `UPDATE jobs SET locked_at=now()-interval '6 minutes' WHERE id=$1`, job.ID); err != nil {
				return err
			}
			_, err := queries.ClaimJob(context.Background(), testClaim(t))
			return err
		},
	}}
	claimed, err := worker.RunOnce(t.Context())
	if err != nil || !claimed {
		t.Fatalf("claimed=%t, err=%v, want a claimed run without error", claimed, err)
	}
	var status string
	var attempts int
	if err := pool.QueryRow(t.Context(), `SELECT status, attempts FROM jobs WHERE id=$1`, job.ID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "running" || attempts != 2 {
		t.Fatalf("job = (%s, %d), want the reclaiming worker still owning it", status, attempts)
	}
}
