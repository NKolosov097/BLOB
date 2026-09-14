package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"blob/server/internal/platform/config"
)

type Server struct {
	http            *http.Server
	shutdownTimeout time.Duration
}

func New(cfg config.HTTP, handler http.Handler) *Server {
	return &Server{
		http: &http.Server{
			Addr:              cfg.Addr,
			Handler:           handler,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
		},
		shutdownTimeout: cfg.ShutdownTimeout,
	}
}

func (s *Server) Run(ctx context.Context) error {
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	shutdownDone := make(chan error, 1)
	go func() {
		<-runCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		shutdownDone <- s.http.Shutdown(shutdownCtx)
	}()

	err := s.http.ListenAndServe()
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	if ctx.Err() != nil {
		return <-shutdownDone
	}
	return nil
}
