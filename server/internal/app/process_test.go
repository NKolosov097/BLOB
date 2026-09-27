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
			cmd.Env = append(os.Environ(), "DATABASE_URL=postgres://"+canary+"%", "OTEL_EXPORTER_OTLP_ENDPOINT=")
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
