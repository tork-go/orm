//go:build integration

// Package postgres_test contains a connectivity smoke test for the pgx
// dependency and the local Docker Postgres setup. It deliberately exercises
// no orm types, since the ORM has no database logic yet, and only runs
// when explicitly requested via the "integration" build tag, since it
// requires a live Postgres instance (see docker-compose.yml at the repo
// root):
//
//	docker compose up -d --wait
//	go test -tags=integration ./tests/postgres/...
package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const defaultDSN = "postgres://tork:tork@localhost:5432/tork_orm_dev?sslmode=disable"

// TestConnectivity proves the pgx driver dependency can open a connection
// to the Docker Postgres instance and run a trivial query. It is
// intentionally independent of any Tork ORM type.
func TestConnectivity(t *testing.T) {
	dsn := os.Getenv("TORK_ORM_POSTGRES_DSN")
	if dsn == "" {
		dsn = defaultDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect(%q) failed: %v", dsn, err)
	}
	defer conn.Close(ctx)

	var got int
	if err := conn.QueryRow(ctx, "SELECT 1").Scan(&got); err != nil {
		t.Fatalf("QueryRow(SELECT 1) failed: %v", err)
	}
	if got != 1 {
		t.Fatalf("SELECT 1 returned %d, want 1", got)
	}
}
