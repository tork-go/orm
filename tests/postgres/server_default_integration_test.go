//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type defaultsRow struct {
	ID      int
	Status  string
	Retries int
	Active  bool
	Ratio   string
	Plain   string
}

type defaultsModel struct {
	orm.Table[defaultsRow]
	ID      *orm.IntColumn
	Status  *orm.StringColumn
	Retries *orm.IntColumn
	Active  *orm.BoolColumn
	Ratio   *orm.StringColumn
	Plain   *orm.StringColumn
}

var defaultsTable = orm.DefineTable[defaultsRow]("sd_defaults",
	func(t *orm.TableBuilder[defaultsRow]) *defaultsModel {
		return &defaultsModel{
			Table: t.Table(),
			ID:    t.Int("id").PrimaryKey(),
			// A string literal, which Postgres prints back with a cast.
			Status: t.String("status").NotNull().ServerDefault("'draft'"),
			// A number, which it prints back bare.
			Retries: t.Int("retries").NotNull().ServerDefault("0"),
			Active:  t.Bool("active").NotNull().ServerDefault("true"),
			// A function call, printed back without a cast.
			Ratio: t.String("ratio").NotNull().ServerDefault("now()::text"),
			// No default at all.
			Plain: t.String("plain").NotNull(),
		}
	})

// Defaults used to be written into DDL and never read back, so the diff
// engine could not compare them. Comparing them without reading them would
// have every migration after the first propose the same change forever,
// which is exactly what this guards: applying the schema and re-diffing
// has to come out empty, and stay empty on a second pass.
//
// The hard part is that Postgres re-prints a default from its parsed form
// rather than storing the text given, adding a cast to the column's type
// while doing so. 'draft' comes back as 'draft'::text.
func TestServerDefault_RoundTripsWithoutDrift(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS sd_defaults CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(defaultsTable)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	ops, _ := migrate.Diff(schema.Schema{}, desired)
	sql, err := migrate.Generate(dialect, ops)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("applying generated SQL failed: %v\n%s", err, sql)
	}

	// Twice, because a normalization that is merely stable rather than
	// correct would still settle after one pass.
	for pass := 1; pass <= 2; pass++ {
		got, err := dialect.Introspect(ctx, conn, []string{"sd_defaults"})
		if err != nil {
			t.Fatalf("pass %d: Introspect failed: %v", pass, err)
		}
		back, err := migrate.Diff(got, desired)
		if err != nil {
			t.Fatalf("pass %d: re-diff failed: %v", pass, err)
		}
		if len(back) != 0 {
			t.Errorf("pass %d: re-diffing produced %d operations, want none:", pass, len(back))
			for _, op := range back {
				t.Errorf("  %#v", op)
			}
		}
		if pass == 1 {
			if more, _ := migrate.Generate(dialect, back); more != "" {
				if _, err := conn.Exec(ctx, more); err != nil {
					t.Fatalf("applying the re-diff failed: %v", err)
				}
			}
		}
	}
}

// A column with no default must not acquire one, and an identity column's
// implicit sequence must not be mistaken for one either.
func TestServerDefault_AbsentAndIdentity(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS sd_defaults CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, _ := schema.ExtractSchema(defaultsTable)
	ops, _ := migrate.Diff(schema.Schema{}, desired)
	sql, _ := migrate.Generate(dialect, ops)
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("applying generated SQL failed: %v", err)
	}

	got, err := dialect.Introspect(ctx, conn, []string{"sd_defaults"})
	if err != nil {
		t.Fatalf("Introspect failed: %v", err)
	}
	byName := map[string]schema.Column{}
	for _, c := range got.Tables[0].Columns {
		byName[c.Name] = c
	}

	if d := byName["plain"].ServerDefault; d != "" {
		t.Errorf("plain has default %q, want none", d)
	}
	// id is GENERATED ALWAYS AS IDENTITY, which Postgres reports with a
	// nextval default over its own sequence.
	if d := byName["id"].ServerDefault; d != "" {
		t.Errorf("id has default %q, want none: an identity column's sequence is not a declared default", d)
	}
	// Introspection reports what Postgres prints, cast and all. Deciding
	// that it matches the declared 'draft' is the diff engine's job.
	if d := byName["status"].ServerDefault; d != "'draft'::text" {
		t.Errorf("status default = %q, want 'draft'::text as Postgres prints it", d)
	}
	if d := byName["retries"].ServerDefault; d != "0" {
		t.Errorf("retries default = %q, want 0", d)
	}
}
