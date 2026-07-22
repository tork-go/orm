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

type rUser struct {
	ID   int
	Name string
	Age  int
}

type rUserModel struct {
	orm.Table[rUser]
	ID   *orm.IntColumn
	Name *orm.StringColumn
	Age  *orm.IntColumn
}

var rUsers = orm.DefineTable[rUser]("r_users", func(t *orm.TableBuilder[rUser]) *rUserModel {
	return &rUserModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
		Age:   t.Int("age").NotNull(),
	}
})

// What Raw renders is checked against the fakes. That its values bind as
// parameters against a real server — matching literally, never executing, and
// numbering in with typed predicates — is what this confirms.
func TestRaw_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS r_users CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(rUsers)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	ops, _ := migrate.Diff(schema.Schema{}, desired)
	ddl, err := migrate.Generate(dialect, ops)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := conn.Exec(ctx, ddl); err != nil {
		t.Fatalf("applying schema failed: %v\n%s", err, ddl)
	}

	db := orm.NewDB(conn, dialect)
	if err := rUsers.With(db).InsertMany(ctx,
		&rUser{Name: "Alice", Age: 30},
		&rUser{Name: "bob", Age: 41},
		&rUser{Name: "Carol", Age: 17},
	); err != nil {
		t.Fatalf("InsertMany failed: %v", err)
	}

	// A function no column exposes, applied to the column.
	t.Run("a function on a column", func(t *testing.T) {
		got, err := rUsers.With(db).Where(orm.Raw("lower(name) = ?", "alice")).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 || got[0].Name != "Alice" {
			t.Errorf("matched %v, want Alice case-insensitively", got)
		}
	})

	// An arithmetic expression, its operand bound.
	t.Run("arithmetic with a bound operand", func(t *testing.T) {
		got, err := rUsers.With(db).Where(orm.Raw("age % ? = 0", 2)).
			OrderBy(rUsers.ID.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 || got[0].Name != "Alice" {
			t.Errorf("matched %v, want only the one even age", got)
		}
	})

	// Numbered in with a typed predicate beside it.
	t.Run("beside a typed predicate", func(t *testing.T) {
		got, err := rUsers.With(db).Where(
			rUsers.Age.Gte(18),
			orm.Raw("lower(name) LIKE ?", "%o%"),
		).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 || got[0].Name != "bob" {
			t.Errorf("matched %v, want only the adult whose name has an o", got)
		}
	})

	// The value is a parameter, so an attempt to smuggle SQL through it matches
	// the literal text and runs nothing. The table surviving is the proof.
	t.Run("a value cannot inject SQL", func(t *testing.T) {
		got, err := rUsers.With(db).Where(
			orm.Raw("name = ?", "Alice'; DROP TABLE r_users; --"),
		).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("matched %v, want nothing for a name no row has", got)
		}
		if n, err := rUsers.With(db).Count(ctx); err != nil || n != 3 {
			t.Fatalf("Count() = %d, %v; want the table intact with 3 rows", n, err)
		}
	})
}
