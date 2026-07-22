package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
)

// Both halves are required, and each is named when it is the one missing.
func TestRecursive_MissingHalves(t *testing.T) {
	db := pg()
	t.Run("anchor", func(t *testing.T) {
		_, _, err := Reports.Recursive(nil, step(db)).SQL()
		assertRecursiveError(t, err, "no anchor")
	})
	t.Run("step", func(t *testing.T) {
		_, _, err := Reports.Recursive(anchor(db), nil).SQL()
		assertRecursiveError(t, err, "no step")
	})
}

// A half that yields the wrong shape is reported against the declaration,
// naming which half it was.
func TestRecursive_ShapeMismatch(t *testing.T) {
	db := pg()
	t.Run("anchor yields too few columns", func(t *testing.T) {
		type short struct{ ID int }
		bad := orm.SelectAs[short](Employees.With(db), Employees.ID)
		_, _, err := Reports.Recursive(bad, step(db)).SQL()
		assertRecursiveError(t, err, "anchor:")
	})
	t.Run("step yields the wrong type", func(t *testing.T) {
		type wrong struct {
			ID        int
			Name      int // the column is a string
			ManagerID *int
		}
		bad := orm.SelectAs[wrong](Employees.With(db), Employees.ID, Employees.ID, Employees.ManagerID)
		_, _, err := Reports.Recursive(anchor(db), bad).SQL()
		assertRecursiveError(t, err, "step:")
	})
}

// A half carrying its own error reports that, rather than a consequence of
// it — the reason From reads its source's error first.
func TestRecursive_HalfCarriesAnError(t *testing.T) {
	db := pg()
	locked := orm.SelectAs[Report](
		Employees.With(db).Where(Employees.Active.Equals(true)).ForUpdate(),
		Employees.ID, Employees.Name, Employees.ManagerID,
	)
	_, _, err := Reports.Recursive(locked, step(db)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the anchor's own error reported")
	}
	if !strings.Contains(err.Error(), "SelectAs") {
		t.Errorf("error = %v, want the source's own complaint", err)
	}
}

// A half that was already unusable when it was built reports that, before
// anything is asked about its shape.
func TestRecursive_HalfWasBuiltBroken(t *testing.T) {
	db := pg()
	// Three fields, one expression: SelectAs records that as its own error
	// the moment it is called.
	broken := orm.SelectAs[Report](Employees.With(db), Employees.ID)

	_, _, err := Reports.Recursive(broken, step(db)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the half's own error reported")
	}
	if !strings.Contains(err.Error(), "expression(s) were given") {
		t.Errorf("error = %v, want SelectAs's own complaint", err)
	}
}

// A step that only fails once it renders — here by naming a column of a
// table it does not read — reports that from the recursion.
func TestRecursive_StepFailsToRender(t *testing.T) {
	db := pg()
	foreign := orm.SelectAs[Report](
		Employees.With(db), Posts.ID, Employees.Name, Employees.ManagerID,
	)
	_, _, err := Reports.Recursive(anchor(db), foreign).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the step's foreign column refused")
	}
	if !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("error = %v, want it to name the table", err)
	}
}

// A zero-valued DerivedTable has no columns to name and no table to define.
func TestRecursive_ZeroValuedModel(t *testing.T) {
	type row struct{ ID int }
	type model struct {
		orm.DerivedTable[row]
		ID *orm.IntColumn
	}
	var m model // never declared

	_, _, err := m.Recursive(anchor(pg()), step(pg())).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the undeclared model refused")
	}
	if !strings.Contains(err.Error(), "DefineDerived") {
		t.Errorf("error = %v, want it to point at DefineDerived", err)
	}
}

// A recursion is a read. Writing through one has no meaning: the rows come
// from a query rather than from storage, which noDerived already says.
func TestRecursive_WritesRejected(t *testing.T) {
	db := pg()
	_, err := Reports.Recursive(anchor(db), step(db)).
		Where(Reports.ID.GreaterThan(0)).
		UpdateAll(context.Background(), Reports.Name.Set("x"))
	if err == nil {
		t.Fatal("UpdateAll() error = nil, want the write refused")
	}
	if !strings.Contains(err.Error(), "derived table") {
		t.Errorf("error = %v", err)
	}
}

func assertRecursiveError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("SQL() error = nil, want %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to mention %q", err, want)
	}
}
