package system

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"blob/server/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

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

// ponytail: fixed 5-minute lease; add heartbeat before introducing handlers that can legitimately run longer
func (j *Jobs) RunOnce(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := j.queries.ExpireJobs(ctx); err != nil {
		return false, err
	}
	job, err := j.queries.ClaimJob(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ctx, span := otel.Tracer("blob/system").Start(ctx, "job.run", trace.WithNewRoot())
	defer func() { span.End() }()
	kind := "unknown"
	if job.Kind == "system.noop" {
		kind = job.Kind
	}
	span.SetAttributes(attribute.String("operation", "run"), attribute.String("job.kind", kind), attribute.Bool("success", false))
	handler, ok := j.handlers[job.Kind]
	if !ok {
		return true, j.fail(ctx, job, "unknown_job_kind")
	}
	if err := handler(ctx, job.Payload); err != nil {
		return true, j.fail(ctx, job, "handler_failed")
	}
	err = j.queries.CompleteJob(ctx, job.ID)
	span.SetAttributes(attribute.Bool("success", err == nil))
	return true, err
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

func (j *Jobs) fail(ctx context.Context, job db.Job, code string) error {
	if code != "unknown_job_kind" && code != "handler_failed" {
		return errors.New("invalid job error code")
	}
	return j.queries.FailJob(ctx, db.FailJobParams{
		ID: job.ID, LastErrorCode: pgtype.Text{String: code, Valid: true},
	})
}
