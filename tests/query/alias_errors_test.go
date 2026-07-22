package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
)

// The mistake this phase exists to catch: a self-referencing relationship
// joined without a name for the far side names one table twice, which no
// database resolves. It used to compile and fail at the database.
func TestJoin_SelfReferencingWithoutAlias(t *testing.T) {
	_, _, err := Employees.With(pg()).Join(Employees.Manager).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the statement refused")
	}
	for _, want := range []string{`"employees"`, "orm.Alias", "JoinAs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %s", err, want)
		}
	}
}

// The same guard catches a table joined twice for two different reasons,
// not only a table joined to itself.
func TestJoinTo_TableAlreadyInTheStatement(t *testing.T) {
	_, _, err := Users.With(pg()).
		JoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
		JoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
		SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the second join refused")
	}
	if !strings.Contains(err.Error(), `"logins"`) {
		t.Errorf("error = %v, want it to name the table joined twice", err)
	}
}

// An alias of some other table cannot stand in for this relationship's far
// side, whatever its columns happen to be called.
func TestJoinAs_AliasOfAnotherTable(t *testing.T) {
	other := orm.Alias(Users, "u2")
	_, _, err := Employees.With(pg()).JoinAs(Employees.Manager, other).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the mismatched alias refused")
	}
	if !strings.Contains(err.Error(), `"employees"`) || !strings.Contains(err.Error(), `"users"`) {
		t.Errorf("error = %v, want it to name both tables", err)
	}
}

// JoinAs takes an alias. A model under its own name would render the same
// broken statement the guard exists to prevent, so it is refused where the
// call can be named.
func TestJoinAs_UnaliasedModel(t *testing.T) {
	_, _, err := Employees.With(pg()).JoinAs(Employees.Manager, Employees).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a table under its own name refused")
	}
	if !strings.Contains(err.Error(), "orm.Alias") {
		t.Errorf("error = %v, want it to point at orm.Alias", err)
	}
}

// A join with no conditions is a cross join, which this package does not
// offer by omission.
func TestJoinTo_NoConditions(t *testing.T) {
	_, _, err := Users.With(pg()).JoinTo(Logins).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a join on nothing refused")
	}
	if !strings.Contains(err.Error(), "cross join") {
		t.Errorf("error = %v, want it to name what a join on nothing would be", err)
	}
}

func TestLeftJoinTo_NoConditions(t *testing.T) {
	_, _, err := Users.With(pg()).LeftJoinTo(Logins).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a join on nothing refused")
	}
}

// A derived table is queried with From rather than joined onto.
func TestJoinTo_DerivedTable(t *testing.T) {
	_, _, err := Users.With(pg()).
		JoinTo(RankedT, Users.Username.Value().Equals(RankedT.Username)).
		SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a derived table refused as a join target")
	}
	if !strings.Contains(err.Error(), "From") {
		t.Errorf("error = %v, want it to point at From", err)
	}
}

// A join needs something to join, whichever end is missing.
func TestJoinTo_NoTable(t *testing.T) {
	_, _, err := Users.With(pg()).JoinTo(nil, Users.ID.Value().Equals(1)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a join with no table refused")
	}
	if !strings.Contains(err.Error(), "no table") {
		t.Errorf("error = %v, want it to say no table was given", err)
	}
}

// A derived table's rows come from a query, which has no table to join
// another onto — the reason Join itself is refused over one.
func TestJoinTo_FromADerivedQuery(t *testing.T) {
	_, _, err := RankedT.From(rankedSource(pg())).
		JoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
		SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a join off a derived table refused")
	}
	if !strings.Contains(err.Error(), "derived table") {
		t.Errorf("error = %v, want it to name the derived table", err)
	}
}

// The ON conditions are checked like any others: a column of a table the
// statement does not read is reported rather than compiled into a
// reference that resolves to nothing.
func TestJoinTo_ConditionOnAnAbsentTable(t *testing.T) {
	_, _, err := Users.With(pg()).
		JoinTo(Logins, Employees.Name.Equals("ada")).
		SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a condition on an absent table refused")
	}
	if !strings.Contains(err.Error(), `belongs to table "employees"`) {
		t.Errorf("error = %v, want it to name the table the column belongs to", err)
	}
}

// JoinAs needs a table for the far side as much as JoinTo does.
func TestJoinAs_NoAlias(t *testing.T) {
	_, _, err := Employees.With(pg()).JoinAs(Employees.Manager, nil).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a join with no alias refused")
	}
	if !strings.Contains(err.Error(), "no table") {
		t.Errorf("error = %v, want it to say no table was given", err)
	}
}

// A model that was never declared has no table to join onto, and says so
// rather than dereferencing its way to a panic.
func TestJoinTo_NilModel(t *testing.T) {
	var missing *LoginModel
	_, _, err := Users.With(pg()).JoinTo(missing, Users.ID.Value().Equals(1)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want an undeclared model refused")
	}
	if !strings.Contains(err.Error(), "DefineTable") {
		t.Errorf("error = %v, want it to point at DefineTable", err)
	}
}

// Every write over an alias is refused: an alias names no stored table to
// write to.
func TestAlias_WritesRejected(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	db := pg()
	ctx := context.Background()

	t.Run("Insert", func(t *testing.T) {
		err := mgr.With(db).Insert(ctx, &Employee{ID: 1, Name: "Ada"})
		assertAliasWriteRefused(t, err)
	})
	t.Run("Update", func(t *testing.T) {
		err := mgr.With(db).Update(ctx, &Employee{ID: 1, Name: "Ada"})
		assertAliasWriteRefused(t, err)
	})
	t.Run("Delete", func(t *testing.T) {
		err := mgr.With(db).Delete(ctx, &Employee{ID: 1})
		assertAliasWriteRefused(t, err)
	})
	t.Run("UpdateAll", func(t *testing.T) {
		_, err := mgr.With(db).Where(mgr.Active.Equals(true)).
			UpdateAll(ctx, mgr.Active.Set(false))
		assertAliasWriteRefused(t, err)
	})
	t.Run("DeleteAll", func(t *testing.T) {
		_, err := mgr.With(db).Where(mgr.Active.Equals(false)).DeleteAll(ctx)
		assertAliasWriteRefused(t, err)
	})
	t.Run("InsertMany", func(t *testing.T) {
		err := mgr.With(db).InsertMany(ctx, &Employee{ID: 1, Name: "Ada"})
		assertAliasWriteRefused(t, err)
	})
}

func assertAliasWriteRefused(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want the write over an alias refused")
	}
	if !strings.Contains(err.Error(), `alias of "employees"`) {
		t.Errorf("error = %v, want it to name the table the alias is of", err)
	}
}

// Reading through an alias is what an alias is for, so the guard is on
// writes alone.
func TestAlias_ReadsAllowed(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	if _, _, err := mgr.With(pg()).Where(mgr.Active.Equals(true)).SQL(); err != nil {
		t.Errorf("SQL() error = %v, want a read through an alias allowed", err)
	}
}
