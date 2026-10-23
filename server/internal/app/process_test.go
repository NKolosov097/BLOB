package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessHidesMalformedDatabaseConfig(t *testing.T) {
	const canary = "db-secret-canary"
	for _, command := range []string{"api", "worker"} {
		t.Run(command, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), command+".exe")
			build := exec.Command("go", "build", "-o", binary, "../../cmd/"+command)
			if err := build.Run(); err != nil {
				t.Fatal("could not build process canary")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary)
			cmd.Env = append(os.Environ(),
				"DATABASE_URL=postgres://"+canary+"%",
				"OTEL_EXPORTER_OTLP_ENDPOINT=",
				"AUTH_PUBLIC_ORIGIN=https://process-test.invalid",
				"AUTH_BROWSER_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			)
			output, err := cmd.CombinedOutput()
			if err == nil || ctx.Err() != nil {
				t.Fatal("process did not fail promptly on malformed database config")
			}
			if !json.Valid(output) {
				t.Fatal("process failure was not one structured event")
			}
			var event map[string]string
			if err := json.Unmarshal(output, &event); err != nil || event["level"] != "error" || event["code"] != "database_config_invalid" {
				t.Fatal("process failure code was not stable")
			}
			if strings.Contains(string(output), canary) {
				t.Fatal("database credential appeared in process output")
			}
		})
	}
}

func TestProcessHidesMalformedAuthConfig(t *testing.T) {
	const canary = "auth-secret-canary"
	binary := filepath.Join(t.TempDir(), "api.exe")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/api")
	if err := build.Run(); err != nil {
		t.Fatal("could not build process canary")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(),
		"DATABASE_URL=postgres://blob:blob@127.0.0.1:1/blob?sslmode=disable",
		"OTEL_EXPORTER_OTLP_ENDPOINT=",
		"AUTH_PUBLIC_ORIGIN=https://process-test.invalid",
		"AUTH_BROWSER_KEY="+canary,
	)
	output, err := cmd.CombinedOutput()
	if err == nil || ctx.Err() != nil {
		t.Fatal("process did not fail promptly on malformed auth config")
	}
	if !json.Valid(output) {
		t.Fatal("process failure was not one structured event")
	}
	var event map[string]string
	if err := json.Unmarshal(output, &event); err != nil || event["level"] != "error" || event["code"] != "identity_config_invalid" {
		t.Fatal("process failure code was not stable")
	}
	if strings.Contains(string(output), canary) {
		t.Fatal("browser key appeared in process output")
	}
}
