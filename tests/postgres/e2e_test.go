//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate/cli"
	"github.com/tork-go/orm/tests/fixtures"
)

// TestEndToEnd_MakeMigrationsUpDownHistory drives the full round trip
// against a real Postgres instance: generate a migration from
// fixtures.Users/Post, apply it, verify the tables and constraints exist
// via a follow-up introspection, roll it back, and verify they're gone.
func TestEndToEnd_MakeMigrationsUpDownHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	// t.Cleanup, not defer: registered first so it runs after (not
	// before) the table-drop cleanup below, which needs conn open.
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	// If the test fails partway (e.g. before "migrate down" runs), leave
	// the database clean for the next run. On the success path, "migrate
	// down base" below already drops these tables and its history row.
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), `DROP TABLE IF EXISTS posts, users CASCADE`)
	})
	if _, err := conn.Exec(ctx, `DROP TABLE IF EXISTS posts, users CASCADE`); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	dir := t.TempDir()
	run := func(args ...string) (stdout, stderr string, code int) {
		var out, errOut bytes.Buffer
		code = cli.RunWithArgs(args, &out, &errOut, dsn(), dir, fixtures.Users, fixtures.Posts)
		return out.String(), errOut.String(), code
	}

	// 1. makemigrations against an empty database: expect a CREATE TABLE
	// migration for both users and posts.
	out, errOut, code := run("makemigrations", "-m", "initial")
	if code != 0 {
		t.Fatalf("makemigrations failed: code=%d out=%q err=%q", code, out, errOut)
	}
	if !strings.Contains(out, "Wrote revision") {
		t.Fatalf("makemigrations stdout = %q, want it to report a written revision", out)
	}

	// 2. migrate up head: apply it.
	if out, errOut, code := run("migrate", "up", "head"); code != 0 {
		t.Fatalf("migrate up failed: code=%d out=%q err=%q", code, out, errOut)
	}

	// 3. Verify via a follow-up introspection that the tables and their
	// constraints actually exist.
	got, err := dialect.Introspect(ctx, conn, []string{"users", "posts"})
	if err != nil {
		t.Fatalf("Introspect after migrate up failed: %v", err)
	}
	if len(got.Tables) != 2 {
		t.Fatalf("got %d tables after migrate up, want 2: %+v", len(got.Tables), got.Tables)
	}
	users := tableNamed(t, got, "users")
	if users.PrimaryKey == nil || len(users.Columns) != 3 {
		t.Errorf("users table after migrate up = %+v, want a primary key and 3 columns", users)
	}
	posts := tableNamed(t, got, "posts")
	if len(posts.ForeignKeys) != 1 || posts.ForeignKeys[0].ReferencedTable != "users" {
		t.Errorf("posts table after migrate up = %+v, want a foreign key referencing users", posts)
	}

	// 4. history: expect one applied revision.
	if out, _, code := run("history"); code != 0 {
		t.Fatalf("history failed: code=%d out=%q", code, out)
	} else if strings.Count(out, "applied") != 1 {
		t.Fatalf("history stdout = %q, want exactly one applied revision", out)
	}

	// 5. migrate down base: roll everything back.
	if out, errOut, code := run("migrate", "down", "base"); code != 0 {
		t.Fatalf("migrate down failed: code=%d out=%q err=%q", code, out, errOut)
	}

	// 6. Verify the tables are gone.
	got, err = dialect.Introspect(ctx, conn, []string{"users", "posts"})
	if err != nil {
		t.Fatalf("Introspect after migrate down failed: %v", err)
	}
	if len(got.Tables) != 0 {
		t.Fatalf("got %d tables after migrate down, want 0: %+v", len(got.Tables), got.Tables)
	}

	// 7. history: expect the revision to show as pending again.
	if out, _, code := run("history"); code != 0 {
		t.Fatalf("history (after rollback) failed: code=%d out=%q", code, out)
	} else if !strings.Contains(out, "pending") {
		t.Errorf("history (after rollback) stdout = %q, want the revision to be pending", out)
	}
}
