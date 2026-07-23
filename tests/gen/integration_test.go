//go:build integration

package gen_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/tork-go/orm/driver"
	_ "github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/migrate/cli"
	"github.com/tork-go/orm/tests/genfixtures"
)

// This is the whole point of the generator, checked against a real
// database: models written in .tork and generated into Go produce
// working migrations through the untouched migration pipeline. Nothing
// here knows the models were generated, which is the guarantee.

const defaultDSN = "postgres://tork:tork@localhost:5432/tork_orm_dev?sslmode=disable"

func dsn() string {
	if v := os.Getenv("TORK_ORM_POSTGRES_DSN"); v != "" {
		return v
	}
	return defaultDSN
}

// connect opens a connection for the housekeeping the tests do around
// the migration engine.
func connect(t *testing.T, ctx context.Context) driver.Conn {
	t.Helper()
	dialect, err := driver.For(dsn())
	if err != nil {
		t.Fatalf("resolving the dialect: %v", err)
	}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	return conn
}

// dropTables clears the generated schema so a rerun starts from the
// same empty database the first run saw.
func dropTables(t *testing.T, ctx context.Context) {
	t.Helper()
	conn := connect(t, ctx)
	defer conn.Close(ctx)

	for _, name := range []string{
		"alphas", "authors", "betas", "documents", "doc_labels",
		"gammas", "labels", "nodes", "pairs", "refs", "slots",
	} {
		if _, err := conn.Exec(ctx, `DROP TABLE IF EXISTS "`+name+`" CASCADE`); err != nil {
			t.Fatalf("dropping %s: %v", name, err)
		}
	}
	if _, err := conn.Exec(ctx, `DROP TYPE IF EXISTS "doc_status" CASCADE`); err != nil {
		t.Fatalf("dropping the enum type: %v", err)
	}
	if _, err := conn.Exec(ctx, `DROP TABLE IF EXISTS "tork_migrations" CASCADE`); err != nil {
		t.Fatalf("dropping the history table: %v", err)
	}
}

// TestGeneratedModels_DriveRealMigrations runs makemigrations against a
// live database with the generated models, applies the result, and
// then requires a second makemigrations to find nothing: the schema
// the migration created has to be the schema the models describe, or
// the diff would keep producing work forever.
func TestGeneratedModels_DriveRealMigrations(t *testing.T) {
	ctx := context.Background()
	dropTables(t, ctx)
	t.Cleanup(func() { dropTables(t, context.Background()) })

	dir := t.TempDir()
	models := genfixtures.AllModels()

	first, err := cli.MakeMigrations(ctx, dsn(), dir, "generated schema", models...)
	if err != nil {
		t.Fatalf("MakeMigrations error = %v", err)
	}
	if first == nil {
		t.Fatal("MakeMigrations found no changes against an empty database")
	}

	// The generated schema's whole vocabulary has to survive the round
	// trip into SQL, since each of these is a separate rendering path.
	for _, want := range []string{
		`CREATE TYPE "doc_status" AS ENUM`,
		`CREATE TABLE "documents"`,
		`GENERATED ALWAYS AS IDENTITY`,
		`VARCHAR(120)`,
		`NUMERIC(10,2)`,
		`JSON`,
		`VARCHAR(20)[]`,
		`PRIMARY KEY ("doc_id", "label_id")`,
		`FOREIGN KEY`,
		`fk_ref_pair`,
		`idx_authors_name_created`,
		`idx_authors_lower_name`,
		`ck_author_rating`,
	} {
		if !strings.Contains(first.UpSQL, want) {
			t.Errorf("the migration is missing %q:\n%s", want, first.UpSQL)
		}
	}

	if err := migrate.Apply(ctx, dsn(), dir); err != nil {
		t.Fatalf("applying the migration: %v\n%s", err, first.UpSQL)
	}

	// The database now matches the models structurally: a second run
	// has no table, column, type, or key left to reconcile. What it
	// does still find is the raw SQL expression churn documented by
	// TestHandwrittenModels_ChurnIdentically, which handwritten models
	// produce just the same and which therefore says nothing about
	// where these models came from.
	second, err := cli.MakeMigrations(ctx, dsn(), dir, "should be settled", models...)
	if err != nil {
		t.Fatalf("the second MakeMigrations failed: %v", err)
	}
	if second != nil {
		for _, unwanted := range []string{
			"CREATE TABLE", "DROP TABLE", "ADD COLUMN", "DROP COLUMN",
			"ALTER COLUMN", "CREATE TYPE", "DROP TYPE", "PRIMARY KEY",
			"FOREIGN KEY", "UNIQUE",
		} {
			if strings.Contains(second.UpSQL, unwanted) {
				t.Errorf("the schema still differs structurally (%s):\n%s", unwanted, second.UpSQL)
			}
		}
	}

	// The schema creating migration carries a down direction too, so
	// generated models are as reversible as handwritten ones.
	for _, want := range []string{
		`DROP TABLE "documents"`,
		`DROP TABLE "doc_labels"`,
		`DROP TYPE "doc_status"`,
	} {
		if !strings.Contains(first.DownSQL, want) {
			t.Errorf("the down migration is missing %q:\n%s", want, first.DownSQL)
		}
	}
}
