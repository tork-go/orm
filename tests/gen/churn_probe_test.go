//go:build integration

package gen_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/migrate/cli"
)

// When makemigrations keeps rewriting an expression index or a check
// constraint, is that the generator's doing or the migration engine's?
// The models below are handwritten, in the ORM's own idiom, carrying
// the constraints the generated ones carry. They churn identically,
// which pins the behavior on the engine: Postgres hands those raw SQL
// expressions back in its own normalized spelling, and the diff has no
// way to know that "lower(name)" and "lower((name)::text)" are the same
// index. The test exists to keep that attribution honest, so nobody
// later mistakes it for something the DSL introduced.

type probeRow struct {
	ID     int      `db:"id"`
	Name   string   `db:"name"`
	Rating *float64 `db:"rating"`
}

type probeModel struct {
	orm.Table[probeRow]
	ID     *orm.IntColumn
	Name   *orm.StringColumn
	Rating *orm.NullableDoubleColumn
}

func (m *probeModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{
		orm.NewIndexDef().On("lower(name)").Named("idx_probe_lower_name"),
	}
}

func (m *probeModel) Checks() []orm.CheckDef {
	return []orm.CheckDef{
		orm.NewCheckDef("rating IS NULL OR rating >= 0").Named("ck_probe_rating"),
	}
}

var probes = orm.DefineTable[probeRow]("probes", func(t *orm.TableBuilder[probeRow]) *probeModel {
	return &probeModel{
		Table:  t.Table(),
		ID:     t.Int("id").PrimaryKey(),
		Name:   t.String("name").NotNull().MaxLen(80),
		Rating: t.NullableDouble("rating"),
	}
})

func TestHandwrittenModels_ChurnIdentically(t *testing.T) {
	ctx := context.Background()
	dropProbe(t, ctx)
	t.Cleanup(func() { dropProbe(t, context.Background()) })

	dir := t.TempDir()
	first, err := cli.MakeMigrations(ctx, dsn(), dir, "probe", probes)
	if err != nil {
		t.Fatalf("MakeMigrations error = %v", err)
	}
	if first == nil {
		t.Fatal("no changes against an empty database")
	}
	if err := migrate.Apply(ctx, dsn(), dir); err != nil {
		t.Fatalf("applying: %v", err)
	}

	second, err := cli.MakeMigrations(ctx, dsn(), dir, "again", probes)
	if err != nil {
		t.Fatalf("the second MakeMigrations failed: %v", err)
	}
	if second == nil {
		t.Fatal("handwritten models settled, so the churn the generated ones show would be the generator's doing after all")
	}
	for _, want := range []string{"DROP INDEX", "CREATE INDEX", "CHECK"} {
		if !strings.Contains(second.UpSQL, want) {
			t.Errorf("the handwritten churn is missing %q:\n%s", want, second.UpSQL)
		}
	}
	// Structurally, though, handwritten models settle exactly as the
	// generated ones do.
	for _, unwanted := range []string{"CREATE TABLE", "ADD COLUMN", "ALTER COLUMN"} {
		if strings.Contains(second.UpSQL, unwanted) {
			t.Errorf("handwritten models differ structurally (%s):\n%s", unwanted, second.UpSQL)
		}
	}
}

func dropProbe(t *testing.T, ctx context.Context) {
	t.Helper()
	conn := connect(t, ctx)
	defer conn.Close(ctx)
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS "probes" CASCADE`,
		`DROP TABLE IF EXISTS "tork_migrations" CASCADE`,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}
