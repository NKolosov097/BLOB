package httpapi

import (
	"context"
	"net/http"

	"blob/server/internal/modules/system"
	"blob/server/internal/platform/requestid"
)

type handler struct {
	health system.HealthChecker
}

func NewHandler(health system.HealthChecker) http.Handler {
	strict := NewStrictHandler(handler{health: health}, nil)
	return requestid.Middleware(HandlerWithOptions(strict, StdHTTPServerOptions{BaseURL: "/api/v1"}))
}

func (h handler) GetLiveness(ctx context.Context, _ GetLivenessRequestObject) (GetLivenessResponseObject, error) {
	if err := h.health.Live(ctx); err != nil {
		return nil, err
	}
	return GetLiveness200JSONResponse{Status: Ok}, nil
}

func (h handler) GetReadiness(ctx context.Context, _ GetReadinessRequestObject) (GetReadinessResponseObject, error) {
	if err := h.health.Ready(ctx); err != nil {
		var response GetReadiness503JSONResponse
		response.Error.Code = "dependency_unavailable"
		response.Error.Message = "Service is not ready"
		response.Error.RequestId = requestid.FromContext(ctx)
		return response, nil
	}
	return GetReadiness200JSONResponse{Status: Ok}, nil
}
