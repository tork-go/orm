package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// An alias is a second name for one table, so a statement reading through
// one names the stored table and then the name it reads it as.
func TestAlias_FromClause(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	sql, _, err := mgr.With(pg()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "name", "active", "manager_id" FROM "employees" AS "mgr"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// The alias's columns are the model's own columns, bound to the new name,
// so every predicate reads exactly as it does on the stored table.
func TestAlias_ColumnsCarryTheAliasName(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	if got := mgr.Name.OwnerTable(); got != "mgr" {
		t.Errorf("OwnerTable() = %q, want %q", got, "mgr")
	}
	if got := mgr.TableName(); got != "mgr" {
		t.Errorf("TableName() = %q, want %q", got, "mgr")
	}
	// The table it was made from is untouched: aliasing declares a second
	// model rather than renaming the first.
	if got := Employees.Name.OwnerTable(); got != "employees" {
		t.Errorf("Employees.Name.OwnerTable() = %q, want %q", got, "employees")
	}
}

// The headline: a relationship whose far side is the declaring table.
func TestJoinAs_SelfJoin(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	sql, args, err := Employees.With(pg()).
		JoinAs(Employees.Manager, mgr).
		Where(mgr.Name.Equals("Ada")).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + employeeCols + ` FROM "employees" ` +
		`JOIN "employees" AS "mgr" ON "mgr"."id" = "employees"."manager_id" ` +
		`WHERE "mgr"."name" = $1`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != "Ada" {
		t.Errorf("args = %v, want [Ada]", args)
	}
}

// A HasMany aliased runs the join the other way: the key is on the far
// side, which is this same table under its second name.
func TestJoinAs_HasManyDirection(t *testing.T) {
	report := orm.Alias(Employees, "report")
	sql, _, err := Employees.With(pg()).JoinAs(Employees.Reports, report).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + employeeCols + ` FROM "employees" ` +
		`JOIN "employees" AS "report" ON "report"."manager_id" = "employees"."id"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// LeftJoinAs keeps an employee whose manager row is not there, whose alias
// columns then come back NULL.
func TestLeftJoinAs_KeepsUnmatchedRows(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	sql, _, err := Employees.With(pg()).
		LeftJoinAs(Employees.Manager, mgr).
		Where(mgr.ManagerID.IsNull()).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + employeeCols + ` FROM "employees" ` +
		`LEFT JOIN "employees" AS "mgr" ON "mgr"."id" = "employees"."manager_id" ` +
		`WHERE "mgr"."manager_id" IS NULL`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// JoinOnAs puts its extra conditions on the ON clause, where JoinOn puts
// its own.
func TestJoinOnAs_ExtraConditionsLandOnON(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	sql, args, err := Employees.With(pg()).
		JoinOnAs(Employees.Manager, mgr, mgr.Active.Equals(true)).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + employeeCols + ` FROM "employees" ` +
		`JOIN "employees" AS "mgr" ON "mgr"."id" = "employees"."manager_id" ` +
		`AND "mgr"."active" = $1`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != true {
		t.Errorf("args = %v, want [true]", args)
	}
}

// The pairing JoinOn's documentation is about: the condition is checked as
// part of the join, so an employee with no active manager still comes back.
func TestLeftJoinOnAs_ConditionStaysOnTheJoin(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	sql, _, err := Employees.With(pg()).
		LeftJoinOnAs(Employees.Manager, mgr, mgr.Active.Equals(true)).
		Where(mgr.ManagerID.IsNull()).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + employeeCols + ` FROM "employees" ` +
		`LEFT JOIN "employees" AS "mgr" ON "mgr"."id" = "employees"."manager_id" ` +
		`AND "mgr"."active" = $1 WHERE "mgr"."manager_id" IS NULL`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// One statement may name the same table more than twice, given a name for
// each, and each join renders in the order it was called.
func TestJoinAs_TwoAliasesOfOneTable(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	report := orm.Alias(Employees, "report")
	sql, _, err := Employees.With(pg()).
		JoinAs(Employees.Manager, mgr).
		JoinAs(Employees.Reports, report).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + employeeCols + ` FROM "employees" ` +
		`JOIN "employees" AS "mgr" ON "mgr"."id" = "employees"."manager_id" ` +
		`JOIN "employees" AS "report" ON "report"."manager_id" = "employees"."id"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// JoinTo joins two tables with no relationship declared between them, on
// conditions written out in full.
func TestJoinTo_UnrelatedTable(t *testing.T) {
	sql, args, err := Users.With(pg()).
		JoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
		Where(Logins.Failed.Equals(true)).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "users"."id", "users"."username", "users"."email", "users"."age", ` +
		`"users"."prefs", "users"."created_at" FROM "users" ` +
		`JOIN "logins" ON "logins"."user_id" = "users"."id" ` +
		`WHERE "logins"."failed" = $1`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != true {
		t.Errorf("args = %v, want [true]", args)
	}
}

// Several conditions are ANDed into the one ON clause, the way a Where's
// several conditions are ANDed into the one WHERE.
func TestJoinTo_SeveralConditions(t *testing.T) {
	sql, _, err := Users.With(pg()).
		JoinTo(Logins,
			Logins.UserID.Value().Equals(Users.ID),
			Logins.Failed.Equals(false),
		).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql,
		`JOIN "logins" ON ("logins"."user_id" = "users"."id" AND "logins"."failed" = $1)`) {
		t.Errorf("SQL() = %s, want both conditions ANDed into the ON clause", sql)
	}
}

func TestLeftJoinTo_Renders(t *testing.T) {
	sql, _, err := Users.With(pg()).
		LeftJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `LEFT JOIN "logins" ON "logins"."user_id" = "users"."id"`) {
		t.Errorf("SQL() = %s, want a LEFT JOIN onto logins", sql)
	}
}

// JoinTo takes an alias as readily as a table, which is what joining one
// table to itself without a relationship needs.
func TestJoinTo_Alias(t *testing.T) {
	other := orm.Alias(Employees, "other")
	sql, _, err := Employees.With(pg()).
		JoinTo(other, other.Name.Value().Equals(Employees.Name)).
		Where(other.ID.Value().NotEquals(Employees.ID)).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql,
		`JOIN "employees" AS "other" ON "other"."name" = "employees"."name"`) {
		t.Errorf("SQL() = %s, want the alias joined under its own name", sql)
	}
}

// Reading both sides of a self join needs SelectAs, for the reason Join's
// own documentation gives: the rows are no longer *E.
func TestJoinAs_SelectAsReadsBothSides(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	type pair struct {
		Employee string
		Manager  string
	}
	sql, _, err := orm.SelectAs[pair](
		Employees.With(pg()).JoinAs(Employees.Manager, mgr),
		Employees.Name, mgr.Name,
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "employees"."name", "mgr"."name" FROM "employees" ` +
		`JOIN "employees" AS "mgr" ON "mgr"."id" = "employees"."manager_id"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// An alias column orders and groups like any other.
func TestJoinAs_OrderAndGroupByAliasColumn(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	type row struct {
		Manager string
		Reports int64
	}
	sql, _, err := orm.SelectAs[row](
		Employees.With(pg()).JoinAs(Employees.Manager, mgr),
		mgr.Name, orm.CountAll(),
	).GroupBy(mgr.Name).OrderBy(mgr.Name.Asc()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `GROUP BY "mgr"."name" ORDER BY "mgr"."name" ASC`) {
		t.Errorf("SQL() = %s, want it grouped and ordered by the alias column", sql)
	}
}

// Count over a self join counts joined rows, the same as Count over any
// other join does.
func TestJoinAs_CountRenders(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{int64(3)})
	db := orm.NewDB(c, postgres.Dialect{})

	mgr := orm.Alias(Employees, "mgr")
	n, err := Employees.With(db).
		JoinAs(Employees.Manager, mgr).
		Where(mgr.Active.Equals(true)).
		Count(context.Background())
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if n != 3 {
		t.Errorf("Count() = %d, want 3", n)
	}
	want := `SELECT COUNT(*) FROM "employees" ` +
		`JOIN "employees" AS "mgr" ON "mgr"."id" = "employees"."manager_id" ` +
		`WHERE "mgr"."active" = $1`
	if got := c.QueryCalls(); len(got) != 1 || got[0] != want {
		t.Errorf("QueryCalls() = %v\nwant   = [%s]", got, want)
	}
}

// An alias is a second handle on one table, so a query through one reads
// the very rows the table holds — nothing about the statement's other
// clauses changes.
func TestAlias_ReadsThroughTheStoredTable(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	sql, args, err := mgr.With(pg()).
		Where(mgr.Active.Equals(true)).
		OrderBy(mgr.Name.Asc()).
		Limit(5).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "name", "active", "manager_id" FROM "employees" AS "mgr" ` +
		`WHERE "active" = $1 ORDER BY "name" ASC LIMIT 5`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != true {
		t.Errorf("args = %v, want [true]", args)
	}
}

// A default scope and a soft-delete column are declared on the model, so an
// alias carries both — filtering on its own columns rather than on the
// stored table's, which this statement does not select from.
func TestAlias_CarriesTheDefaultScope(t *testing.T) {
	recent := orm.Alias(ScopedPosts, "recent")
	sql, _, err := recent.With(pg()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `FROM "scoped_posts" AS "recent"`) {
		t.Errorf("SQL() = %s, want the stored table under the alias", sql)
	}
	if !strings.Contains(sql, `"published" = $1`) ||
		!strings.Contains(sql, `"deleted_at" IS NULL`) {
		t.Errorf("SQL() = %s, want the scope and the soft-delete filter carried over", sql)
	}
}

// Unscoped reaches an alias the same way it reaches the table it was made
// from.
func TestAlias_Unscoped(t *testing.T) {
	recent := orm.Alias(ScopedPosts, "recent")
	sql, _, err := recent.With(pg()).Unscoped().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(sql, "WHERE") {
		t.Errorf("SQL() = %s, want no filter left once unscoped", sql)
	}
}

// Aliasing an alias names the stored table, so a chain of them never grows
// a chain of names.
func TestAlias_OfAnAliasPointsAtStorage(t *testing.T) {
	first := orm.Alias(Employees, "first")
	second := orm.Alias(first, "second")
	sql, _, err := second.With(pg()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `FROM "employees" AS "second"`) {
		t.Errorf("SQL() = %s, want the stored table under the second alias", sql)
	}
}

// An alias name is an identifier like a table's own, quoted by the dialect
// on the way into the statement. A name carrying a quote is escaped rather
// than closing the one around it, so nothing a caller passes here can end
// the identifier and start writing SQL of its own.
func TestAlias_NameIsQuotedNotInterpolated(t *testing.T) {
	hostile := orm.Alias(Employees, `m" ON 1=1 --`)
	sql, _, err := hostile.With(pg()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `FROM "employees" AS "m"" ON 1=1 --"`) {
		t.Errorf("SQL() = %s, want the quote in the alias doubled rather than closing", sql)
	}
	// The column qualifies under the same escaped name, so a joined
	// statement stays one statement.
	joined, _, err := Employees.With(pg()).
		JoinAs(Employees.Manager, hostile).
		Where(hostile.Name.Equals("ada")).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(joined, `WHERE "m"" ON 1=1 --"."name" = $1`) {
		t.Errorf("SQL() = %s, want the condition qualified by the escaped name", joined)
	}
}

// A relationship reached through an alias correlates on the alias's own
// columns, since that is the name this statement reads that table by.
func TestAlias_HasCorrelatesOnTheAlias(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	sql, _, err := mgr.With(pg()).Where(orm.Has(mgr.Reports)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "name", "active", "manager_id" FROM "employees" AS "mgr" ` +
		`WHERE EXISTS (SELECT 1 FROM "employees" ` +
		`WHERE "employees"."manager_id" = "mgr"."id")`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// Loading through an alias fetches from the stored table the relationship
// names, matched against keys read out of the aliased rows.
func TestAlias_LoadRunsAgainstTheStoredTable(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "ada", true, (*int)(nil)})
	c.QueueRows([]any{2, "ben", true, ptr(1)})
	db := orm.NewDB(c, postgres.Dialect{})

	mgr := orm.Alias(Employees, "mgr")
	rows, err := mgr.With(db).Load(mgr.Reports).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(rows) != 1 || len(rows[0].Reports) != 1 || rows[0].Reports[0].Name != "ben" {
		t.Fatalf("All() = %+v, want ada carrying one report", rows)
	}
	calls := c.QueryCalls()
	if len(calls) != 2 {
		t.Fatalf("QueryCalls() = %v, want the read and its load", calls)
	}
	if !strings.Contains(calls[0], `FROM "employees" AS "mgr"`) {
		t.Errorf("read = %s, want it through the alias", calls[0])
	}
	if !strings.Contains(calls[1], `FROM "employees" WHERE "manager_id" IN`) {
		t.Errorf("load = %s, want it against the stored table", calls[1])
	}
}

// The registry maps a row type to the one table declared for it. An alias
// staying out of it is what keeps a relationship naming Employee resolving
// to the stored table however many aliases exist.
func TestAlias_DoesNotShadowTheStoredTable(t *testing.T) {
	orm.Alias(Employees, "shadow")
	sql, _, err := Employees.With(pg()).Join(Employees.Reports).SQL()
	if err == nil {
		t.Fatalf("SQL() = %s, want the unaliased self join refused", sql)
	}
	// The relationship still resolves to "employees"; what fails is naming
	// it twice, which is the guard rather than a lookup gone wrong.
	if !strings.Contains(err.Error(), `names table "employees"`) {
		t.Errorf("error = %v, want it to name the stored table", err)
	}
}

// A query branched after a join does not leak it into its sibling, which
// every builder method on Filtered owes its caller.
func TestJoinAs_DoesNotLeakIntoSiblings(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	base := Employees.With(pg()).Where(Employees.Active.Equals(true))
	joined := base.JoinAs(Employees.Manager, mgr)

	plain, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(plain, "JOIN") {
		t.Errorf("SQL() = %s, want the branch it was taken from left unjoined", plain)
	}
	withJoin, _, err := joined.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(withJoin, `JOIN "employees" AS "mgr"`) {
		t.Errorf("SQL() = %s, want the branch itself joined", withJoin)
	}
}
