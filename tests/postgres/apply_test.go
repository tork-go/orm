//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/migrate/cli"
	"github.com/tork-go/orm/tests/fixtures"
)

// TestApply_BringsSchemaUpToDate proves migrate.Apply, the one-call
// convenience entrypoint meant to be called at an application's startup
// (mirroring SQLModel.metadata.create_all(engine) or Drizzle's
// migrate(db, {...})), actually applies a generated migration against a
// real database.
func TestApply_BringsSchemaUpToDate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	// t.Cleanup, not defer: registered first so it runs after (not
	// before) the cleanups below, which need conn open.
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	t.Cleanup(func() {
		_ = conn.Exec(context.Background(), `DROP TABLE IF EXISTS users CASCADE`)
	})
	if err := conn.Exec(ctx, `DROP TABLE IF EXISTS users CASCADE`); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	dir := t.TempDir()
	m, err := cli.MakeMigrations(ctx, dialect, dsn(), dir, "add users", fixtures.User)
	if err != nil {
		t.Fatalf("MakeMigrations failed: %v", err)
	}
	// Apply below never rolls back (that's not what this test is
	// checking), so its history row would otherwise outlive the test.
	t.Cleanup(func() { _ = dialect.DeleteHistoryRow(context.Background(), conn, m.Revision) })

	if err := migrate.Apply(ctx, dialect, dsn(), dir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	got, err := dialect.Introspect(ctx, conn, []string{"users"})
	if err != nil {
		t.Fatalf("Introspect after Apply failed: %v", err)
	}
	if len(got.Tables) != 1 {
		t.Fatalf("got %d tables after Apply, want 1 (users): %+v", len(got.Tables), got.Tables)
	}

	// Calling Apply again with nothing new pending must be a no-op, not
	// an error (this is what makes it safe to call on every app startup).
	if err := migrate.Apply(ctx, dialect, dsn(), dir); err != nil {
		t.Fatalf("second Apply call (nothing pending) failed: %v", err)
	}
}
