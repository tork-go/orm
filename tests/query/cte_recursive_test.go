package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// The rows a walk down the org chart yields: an employee, and how they were
// reached.
type Report struct {
	ID        int
	Name      string
	ManagerID *int
}

type reportModel struct {
	orm.DerivedTable[Report]
	ID        *orm.IntColumn
	Name      *orm.StringColumn
	ManagerID *orm.NullableIntColumn
}

var Reports = orm.DefineDerived[Report]("reports", func(t *orm.TableBuilder[Report]) *reportModel {
	return &reportModel{
		DerivedTable: t.Derived(),
		ID:           t.Int("id"),
		Name:         t.String("name"),
		ManagerID:    t.NullableInt("manager_id"),
	}
})

// anchor is everyone at the top of the chart, and step everyone who reports
// to somebody already found. Written once here since every case below needs
// both.
func anchor(db *orm.DB) orm.DerivedSource {
	return orm.SelectAs[Report](
		Employees.With(db).Where(Employees.ManagerID.IsNull()),
		Employees.ID, Employees.Name, Employees.ManagerID,
	)
}

func step(db *orm.DB) orm.DerivedSource {
	return orm.SelectAs[Report](
		Employees.With(db).JoinTo(Reports, Reports.ID.Value().Equals(Employees.ManagerID)),
		Employees.ID, Employees.Name, Employees.ManagerID,
	)
}

// The whole shape: a WITH RECURSIVE naming its own columns, an anchor, and a
// step that reads the table being defined.
func TestRecursive_Renders(t *testing.T) {
	db := pg()
	sql, _, err := Reports.Recursive(anchor(db), step(db)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `WITH RECURSIVE "reports"("id", "name", "manager_id") AS (` +
		`SELECT "id", "name", "manager_id" FROM "employees" WHERE "manager_id" IS NULL` +
		` UNION ALL ` +
		`SELECT "employees"."id", "employees"."name", "employees"."manager_id" ` +
		`FROM "employees" JOIN "reports" ON "reports"."id" = "employees"."manager_id"` +
		`) SELECT "id", "name", "manager_id" FROM "reports"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// RecursiveDistinct pools with UNION, which is what ends a walk over a graph
// that loops back on itself.
func TestRecursive_Distinct(t *testing.T) {
	db := pg()
	sql, _, err := Reports.RecursiveDistinct(anchor(db), step(db)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(sql, "UNION ALL") || !strings.Contains(sql, " UNION ") {
		t.Errorf("SQL() = %s, want the halves pooled with UNION", sql)
	}
}

// The result is an ordinary read over the finished pool.
func TestRecursive_ReadsLikeAnyTable(t *testing.T) {
	db := pg()
	sql, args, err := Reports.Recursive(anchor(db), step(db)).
		Where(Reports.Name.Contains("a")).
		OrderBy(Reports.Name.Asc()).
		Limit(10).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `FROM "reports" WHERE "name" LIKE $1 ESCAPE '\' ORDER BY "name" ASC LIMIT 10`) {
		t.Errorf("SQL() = %s", sql)
	}
	if len(args) != 1 {
		t.Errorf("args = %v, want the pattern bound once", args)
	}
}

// A projection over the pool, which is how a recursive read is usually
// narrowed to the columns that matter.
func TestRecursive_Projection(t *testing.T) {
	db := pg()
	type row struct {
		Name string
		N    int64
	}
	sql, _, err := orm.SelectAs[row](
		Reports.Recursive(anchor(db), step(db)),
		Reports.Name, orm.CountAll(),
	).GroupBy(Reports.Name).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `WITH RECURSIVE "reports"`) ||
		!strings.HasSuffix(sql, `FROM "reports" GROUP BY "name"`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// Both halves bind against the one statement, in the order they appear, and
// the read's own conditions bind after them.
func TestRecursive_PlaceholdersNumberInOrder(t *testing.T) {
	db := pg()
	deep := orm.SelectAs[Report](
		Employees.With(db).
			JoinTo(Reports, Reports.ID.Value().Equals(Employees.ManagerID)).
			Where(Employees.Active.Equals(true)),
		Employees.ID, Employees.Name, Employees.ManagerID,
	)
	sql, args, err := Reports.Recursive(
		orm.SelectAs[Report](
			Employees.With(db).Where(Employees.Name.Equals("ada")),
			Employees.ID, Employees.Name, Employees.ManagerID,
		),
		deep,
	).Where(Reports.Name.NotEquals("ada")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if len(args) != 3 || args[0] != "ada" || args[1] != true || args[2] != "ada" {
		t.Errorf("args = %v, want [ada true ada]", args)
	}
	if !strings.Contains(sql, `"name" = $1`) ||
		!strings.Contains(sql, `"employees"."active" = $2`) ||
		!strings.HasSuffix(sql, `"name" <> $3`) {
		t.Errorf("SQL() = %s, want the placeholders numbered in the order they read", sql)
	}
}

// A recursion alongside an ordinary CTE: one WITH list, and the RECURSIVE
// keyword belongs to the list rather than to either definition.
func TestRecursive_BesideAnOrdinaryCTE(t *testing.T) {
	db := pg()
	sql, _, err := Reports.Recursive(anchor(db), step(db)).
		With("recent", orm.Select(Employees.With(db).Where(Employees.Active.Equals(true)), Employees.ID)).
		Where(Reports.ID.InQuery(orm.CTE[int]("recent"))).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasPrefix(sql, `WITH RECURSIVE "reports"(`) {
		t.Errorf("SQL() = %s, want one recursive WITH list", sql)
	}
	if !strings.Contains(sql, `, "recent" AS (SELECT "employees"."id" FROM "employees"`) {
		t.Errorf("SQL() = %s, want the ordinary CTE in the same list", sql)
	}
}

// It runs, not merely compiles.
func TestRecursive_All(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "ada", (*int)(nil)}, []any{2, "ben", ptr(1)})
	db := orm.NewDB(c, postgres.Dialect{})

	rows, err := Reports.Recursive(anchor(db), step(db)).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "ada" || rows[1].ManagerID == nil {
		t.Errorf("All() = %+v", rows)
	}
	if got := c.QueryCalls(); len(got) != 1 || !strings.HasPrefix(got[0], "WITH RECURSIVE") {
		t.Errorf("QueryCalls() = %v", got)
	}
}

// Counting a recursion counts the pool.
func TestRecursive_Count(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{int64(7)})
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Reports.Recursive(anchor(db), step(db)).Count(context.Background())
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if n != 7 {
		t.Errorf("Count() = %d, want 7", n)
	}
	if got := c.QueryCalls(); len(got) != 1 ||
		!strings.Contains(got[0], `SELECT COUNT(*) FROM "reports"`) {
		t.Errorf("QueryCalls() = %v", got)
	}
}
