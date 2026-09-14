package testdb

import (
	"os"
	"testing"
)

func URL(t *testing.T) string {
	t.Helper()
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		return url
	}
	if os.Getenv("CI") == "true" {
		t.Fatal("TEST_DATABASE_URL is required in CI")
	}
	t.Skip("TEST_DATABASE_URL is not set")
	return ""
}
