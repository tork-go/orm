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

// A tree to walk down, and a pair of nodes pointing at each other to prove a
// cycle is what it looks like.
type rNode struct {
	ID       int
	Name     string
	ParentID *int
}

type rNodeModel struct {
	orm.Table[rNode]
	ID       *orm.IntColumn
	Name     *orm.StringColumn
	ParentID *orm.NullableIntColumn
}

var rNodes = orm.DefineTable[rNode]("r_nodes", func(t *orm.TableBuilder[rNode]) *rNodeModel {
	return &rNodeModel{
		Table:    t.Table(),
		ID:       t.Int("id").PrimaryKey(),
		Name:     t.String("name").NotNull().MaxLen(20),
		ParentID: t.NullableInt("parent_id"),
	}
})

// The shape a walk yields: the node, and how deep it was found.
type rWalk struct {
	ID    int
	Name  string
	Depth int
}

type rWalkModel struct {
	orm.DerivedTable[rWalk]
	ID    *orm.IntColumn
	Name  *orm.StringColumn
	Depth *orm.IntColumn
}

var rWalks = orm.DefineDerived[rWalk]("walk", func(t *orm.TableBuilder[rWalk]) *rWalkModel {
	return &rWalkModel{
		DerivedTable: t.Derived(),
		ID:           t.Int("id"),
		Name:         t.String("name"),
		Depth:        t.Int("depth"),
	}
})

// A recursion is the one query shape whose compile test proves least: the
// SQL parses either way, and only a database says whether it terminates and
// what it found.
func TestRecursiveCTE_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS r_nodes CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(rNodes)
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

	// A tree three deep under root, a second root with one child, and two
	// nodes that point at each other — a cycle no tree walk should reach.
	if _, err := conn.Exec(ctx, `
		INSERT INTO r_nodes (id, name, parent_id) OVERRIDING SYSTEM VALUE VALUES
			(1, 'root',   NULL),
			(2, 'a',      1),
			(3, 'b',      1),
			(4, 'a1',     2),
			(5, 'a1x',    4),
			(6, 'other',  NULL),
			(7, 'other1', 6),
			(8, 'loopA',  9),
			(9, 'loopB',  8)`); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	db := orm.NewDB(conn, dialect)

	// Everything under one root, to any depth, carrying how deep it was
	// found — the query a recursion exists for.
	t.Run("walks the tree and counts depth", func(t *testing.T) {
		anchor := orm.SelectAs[rWalk](
			rNodes.With(db).Where(rNodes.Name.Equals("root")),
			rNodes.ID, rNodes.Name, orm.Lit(0),
		)
		step := orm.SelectAs[rWalk](
			rNodes.With(db).JoinTo(rWalks, rWalks.ID.Value().Equals(rNodes.ParentID)),
			rNodes.ID, rNodes.Name, rWalks.Depth.Plus(1),
		)

		got, err := rWalks.Recursive(anchor, step).
			OrderBy(rWalks.Depth.Asc(), rWalks.Name.Asc()).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}

		type pair struct {
			name  string
			depth int
		}
		want := []pair{{"root", 0}, {"a", 1}, {"b", 1}, {"a1", 2}, {"a1x", 3}}
		if len(got) != len(want) {
			t.Fatalf("All() returned %d rows, want %d: %+v", len(got), len(want), got)
		}
		for i, w := range want {
			if got[i].Name != w.name || got[i].Depth != w.depth {
				t.Errorf("row %d = %s at %d, want %s at %d",
					i, got[i].Name, got[i].Depth, w.name, w.depth)
			}
		}
	})

	// The pool is an ordinary table to everything downstream: filter it,
	// count it, project over it.
	t.Run("the pool reads like a table", func(t *testing.T) {
		anchor := orm.SelectAs[rWalk](
			rNodes.With(db).Where(rNodes.ParentID.IsNull()),
			rNodes.ID, rNodes.Name, orm.Lit(0),
		)
		step := orm.SelectAs[rWalk](
			rNodes.With(db).JoinTo(rWalks, rWalks.ID.Value().Equals(rNodes.ParentID)),
			rNodes.ID, rNodes.Name, rWalks.Depth.Plus(1),
		)

		deep, err := rWalks.Recursive(anchor, step).
			Where(rWalks.Depth.GreaterOrEqual(2)).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(deep) != 2 {
			t.Errorf("All() = %+v, want the two nodes below depth 2", deep)
		}

		n, err := rWalks.Recursive(anchor, step).Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		// Both roots and their descendants: root, a, b, a1, a1x, other,
		// other1. The cycle is unreachable from either root.
		if n != 7 {
			t.Errorf("Count() = %d, want 7", n)
		}
	})

	// A cycle is what UNION ALL cannot survive and UNION can: the distinct
	// form drops a row already found, so the walk closes instead of looping.
	t.Run("a cycle ends under RecursiveDistinct", func(t *testing.T) {
		anchor := orm.SelectAs[rWalk](
			rNodes.With(db).Where(rNodes.Name.Equals("loopA")),
			rNodes.ID, rNodes.Name, orm.Lit(0),
		)
		// From loopA the walk reaches loopB, whose child is loopA again.
		// The depth stays 0 so that a row found twice really is the same
		// row: what is being tested is that the walk ends at all.
		step := orm.SelectAs[rWalk](
			rNodes.With(db).JoinTo(rWalks, rWalks.ID.Value().Equals(rNodes.ParentID)),
			rNodes.ID, rNodes.Name, orm.Lit(0),
		)

		got, err := rWalks.RecursiveDistinct(anchor, step).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 2 {
			t.Errorf("All() = %+v, want the two nodes of the cycle, each once", got)
		}
	})
}
