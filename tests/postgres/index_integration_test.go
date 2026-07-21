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

type idxRow struct {
	ID        int
	Email     string
	DeletedAt *string
	Tenant    int
}

type idxModel struct {
	orm.Table[idxRow]
	ID        *orm.IntColumn
	Email     *orm.StringColumn
	DeletedAt *orm.NullableStringColumn
	Tenant    *orm.IntColumn
}

func (m *idxModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{
		// A partial index, the common shape for soft deletes.
		orm.NewIndexDef(m.Tenant).Where("deleted_at IS NULL"),
		// An expression index, which has no column list to name it from.
		orm.NewIndexDef().On("lower(email)").Named("ix_idx_rows_lower_email"),
		// A plain index, to show the three coexist.
		orm.NewIndexDef(m.Email),
	}
}

var idxTable = orm.DefineTable[idxRow]("idx_rows", func(t *orm.TableBuilder[idxRow]) *idxModel {
	return &idxModel{
		Table:     t.Table(),
		ID:        t.Int("id").PrimaryKey(),
		Email:     t.String("email").NotNull(),
		DeletedAt: t.NullableString("deleted_at"),
		Tenant:    t.Int("tenant").NotNull(),
	}
})

// Partial and expression indexes used to be excluded from introspection
// outright: schema.Index could express neither, so a partial index read
// back without its predicate would compare equal to a full one. Both now
// round trip, which is what re-diffing to nothing proves.
func TestIndexes_PartialAndExpressionRoundTrip(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS idx_rows CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(idxTable)
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

	got, err := dialect.Introspect(ctx, conn, []string{"idx_rows"})
	if err != nil {
		t.Fatalf("Introspect failed: %v", err)
	}
	if len(got.Tables) != 1 {
		t.Fatalf("introspected %d tables, want 1", len(got.Tables))
	}
	if n := len(got.Tables[0].Indexes); n != 3 {
		t.Fatalf("introspected %d indexes, want 3: %+v", n, got.Tables[0].Indexes)
	}

	var partial, expression, plain bool
	for _, ix := range got.Tables[0].Indexes {
		switch {
		case ix.Where != "":
			partial = true
			if len(ix.Columns) != 1 || ix.Columns[0] != "tenant" {
				t.Errorf("partial index columns = %v, want [tenant]", ix.Columns)
			}
		case len(ix.Expressions) > 0:
			expression = true
			if len(ix.Columns) != 0 {
				t.Errorf("expression index also reported columns %v", ix.Columns)
			}
		default:
			plain = true
		}
	}
	if !partial || !expression || !plain {
		t.Errorf("partial=%v expression=%v plain=%v, want all three", partial, expression, plain)
	}

	// Twice, so a representation that merely settles rather than being
	// correct cannot pass.
	for pass := 1; pass <= 2; pass++ {
		got, err := dialect.Introspect(ctx, conn, []string{"idx_rows"})
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
	}
}
