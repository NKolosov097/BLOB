package database

import (
	"context"
	"testing"

	"blob/server/internal/testdb"
)

func TestOpenAndPing(t *testing.T) {
	url := testdb.URL(t)
	pool, err := Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}
