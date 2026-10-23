package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"blob/server/internal/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// handlerTimeout bounds one handler run well below the five-minute lease, so
// a stuck handler releases the job for reclaim instead of holding it until the
// lease expires. The remaining margin covers the completion write.
const handlerTimeout = 2 * time.Minute

// Handler must be idempotent: jobs are delivered at least once.
type Handler func(context.Context, json.RawMessage) error

type Jobs struct {
	queries  *db.Queries
	handlers map[string]Handler
}

func NewJobs(queries *db.Queries) *Jobs {
	return &Jobs{queries: queries, handlers: map[string]Handler{
		"system.noop": func(context.Context, json.RawMessage) error { return nil },
	}}
}

// Register adds a job kind at composition time. It is not safe to call once
// Run has started; composition roots register every handler up front.
func (j *Jobs) Register(kind string, handler Handler) error {
	if kind == "" || handler == nil {
		return errors.New("system: job kind and handler are required")
	}
	if _, taken := j.handlers[kind]; taken {
		return fmt.Errorf("system: job kind %q already registered", kind)
	}
	j.handlers[kind] = handler
	return nil
}

// ponytail: fixed 5-minute lease; add heartbeat before introducing handlers that can legitimately run longer
func (j *Jobs) RunOnce(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := j.queries.ExpireJobs(ctx); err != nil {
		return false, err
	}
	claim, err := uuid.NewV7()
	if err != nil {
		return false, err
	}
	claimToken := pgtype.UUID{Bytes: claim, Valid: true}
	job, err := j.queries.ClaimJob(ctx, claimToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ctx, span := otel.Tracer("blob/system").Start(ctx, "job.run", trace.WithNewRoot())
	defer func() { span.End() }()
	handler, known := j.handlers[job.Kind]
	kind := "unknown"
	if known {
		kind = job.Kind
	}
	span.SetAttributes(attribute.String("operation", "run"), attribute.String("job.kind", kind), attribute.Bool("success", false))
	if !known {
		return true, j.fail(ctx, span, job, claimToken, "unknown_job_kind")
	}
	handlerCtx, cancel := context.WithTimeout(ctx, handlerTimeout)
	defer cancel()
	if err := handler(handlerCtx, job.Payload); err != nil {
		return true, j.fail(ctx, span, job, claimToken, "handler_failed")
	}
	rows, err := j.queries.CompleteJob(ctx, db.CompleteJobParams{ID: job.ID, ClaimToken: claimToken})
	if err != nil {
		return true, err
	}
	// Zero rows means another worker reclaimed the expired lease and now owns
	// the outcome. This worker's result is void, not an error.
	span.SetAttributes(attribute.Bool("claim.lost", rows == 0), attribute.Bool("success", rows == 1))
	return true, nil
}

func (j *Jobs) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		claimed, err := j.RunOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		if !claimed {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
		}
	}
	return nil
}

func (j *Jobs) fail(ctx context.Context, span trace.Span, job db.Job, claimToken pgtype.UUID, code string) error {
	if code != "unknown_job_kind" && code != "handler_failed" {
		return errors.New("invalid job error code")
	}
	rows, err := j.queries.FailJob(ctx, db.FailJobParams{
		ID: job.ID, ClaimToken: claimToken,
		LastErrorCode: pgtype.Text{String: code, Valid: true},
	})
	if err != nil {
		return err
	}
	span.SetAttributes(attribute.Bool("claim.lost", rows == 0))
	return nil
}
