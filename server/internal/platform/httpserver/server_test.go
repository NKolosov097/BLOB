package httpserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"blob/server/internal/platform/config"
)

func TestNewAppliesHTTPConfig(t *testing.T) {
	cfg := config.HTTP{
		Addr:              "127.0.0.1:0",
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
		ShutdownTimeout:   5 * time.Second,
	}
	handler := http.NewServeMux()

	server := New(cfg, handler)

	if server.http.Addr != cfg.Addr || server.http.Handler != handler ||
		server.http.ReadHeaderTimeout != cfg.ReadHeaderTimeout ||
		server.http.ReadTimeout != cfg.ReadTimeout ||
		server.http.WriteTimeout != cfg.WriteTimeout ||
		server.http.IdleTimeout != cfg.IdleTimeout ||
		server.shutdownTimeout != cfg.ShutdownTimeout {
		t.Fatalf("server does not match config: %+v", server.http)
	}
}

func TestRunDrainsInFlightRequestOnShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := config.HTTP{
		Addr:              addr,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
		ShutdownTimeout:   2 * time.Second,
	}
	started := make(chan struct{})
	release := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		select {
		case release <- struct{}{}:
		default:
		}
	})

	runDone := make(chan error, 1)
	go func() { runDone <- New(cfg, handler).Run(ctx) }()
	requestDone := make(chan error, 1)
	go func() {
		client := http.Client{Timeout: 3 * time.Second}
		deadline := time.Now().Add(2 * time.Second)
		for {
			response, err := client.Get("http://" + addr)
			if err == nil {
				_ = response.Body.Close()
				if response.StatusCode != http.StatusNoContent {
					err = fmt.Errorf("got status %d", response.StatusCode)
				}
				requestDone <- err
				return
			}
			if time.Now().After(deadline) {
				requestDone <- err
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	select {
	case <-started:
	case err := <-requestDone:
		t.Fatalf("request did not reach handler: %v", err)
	case err := <-runDone:
		t.Fatalf("server stopped before request: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach handler")
	}

	cancel()
	select {
	case err := <-runDone:
		t.Fatalf("Run returned before active request completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	release <- struct{}{}
	select {
	case err := <-requestDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request did not complete")
	}

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not finish shutdown")
	}
}
