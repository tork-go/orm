package query_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// Where NULLs sort is the dialect's to write, so the clause comes back
// spelled the way that database spells it.
func TestNullsOrder_Renders(t *testing.T) {
	tests := []struct {
		name string
		ord  orm.Ordering
		want string
	}{
		{"ascending nulls first", Users.Email.Asc().NullsFirst(), `"email" ASC NULLS FIRST`},
		{"ascending nulls last", Users.Email.Asc().NullsLast(), `"email" ASC NULLS LAST`},
		{"descending nulls first", Users.Email.Desc().NullsFirst(), `"email" DESC NULLS FIRST`},
		{"descending nulls last", Users.Email.Desc().NullsLast(), `"email" DESC NULLS LAST`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := Users.With(pg()).OrderBy(tt.ord).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if !strings.HasSuffix(sql, "ORDER BY "+tt.want) {
				t.Errorf("SQL() = %s\nwant it to end with ORDER BY %s", sql, tt.want)
			}
		})
	}
}

// Left alone, the ordering says nothing about NULLs and the database
// applies its own rule — which is what every ordering did before this.
func TestNullsOrder_DefaultSaysNothing(t *testing.T) {
	sql, _, err := Users.With(pg()).OrderBy(Users.Email.Asc()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(sql, "NULLS") {
		t.Errorf("SQL() = %s, want no NULLS clause unless asked for", sql)
	}
}

// Another dialect spells it differently, which is the point of routing it
// through the dialect rather than appending a fixed suffix.
func TestNullsOrder_DialectSpellsIt(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), fakedriver.NewDialect())
	sql, _, err := Users.With(db).OrderBy(Users.Email.Asc().NullsFirst()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, "[email] ASC NULLS HIGH") {
		t.Errorf("SQL() = %s, want the fake dialect's own spelling", sql)
	}
}

// A database that cannot place NULLs says so, rather than sorting them
// somewhere the caller did not ask for.
func TestNullsOrder_DialectWithout(t *testing.T) {
	d := fakedriver.NewDialect()
	d.NoNullsOrder = true
	db := orm.NewDB(fakedriver.NewConn(), d)

	_, _, err := Users.With(db).OrderBy(Users.Email.Asc().NullsLast()).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the unsupported clause reported")
	}
	if !strings.Contains(err.Error(), "where NULLs sort") {
		t.Errorf("error = %v, want it to name the operation", err)
	}
}

// An expression's ordering takes the same placement a column's does.
func TestNullsOrder_OverAnExpression(t *testing.T) {
	sql, _, err := Users.With(pg()).OrderBy(orm.Lower(Users.Email).Desc().NullsFirst()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `ORDER BY LOWER("email") DESC NULLS FIRST`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// A window's own ordering places NULLs too, since both render through the
// same term.
func TestNullsOrder_InsideAWindow(t *testing.T) {
	type row struct{ N int64 }
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.RowNumber().OrderBy(Users.Email.Asc().NullsLast()),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `ROW_NUMBER() OVER (ORDER BY "email" ASC NULLS LAST)`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// Ordering is a value, so placing NULLs on one copy leaves the other alone.
func TestNullsOrder_DoesNotMutateTheOrdering(t *testing.T) {
	base := Users.Email.Asc()
	withNulls := base.NullsFirst()

	plain, _, err := Users.With(pg()).OrderBy(base).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(plain, "NULLS") {
		t.Errorf("SQL() = %s, want the ordering it was taken from unchanged", plain)
	}
	placed, _, err := Users.With(pg()).OrderBy(withNulls).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(placed, "NULLS FIRST") {
		t.Errorf("SQL() = %s, want the copy placed", placed)
	}
}

// It reaches every read that orders rows, not only Filtered's own.
func TestNullsOrder_InAProjection(t *testing.T) {
	type row struct{ Email *string }
	sql, _, err := orm.SelectAs[row](Users.With(pg()), Users.Email).
		OrderBy(Users.Email.Desc().NullsLast()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `ORDER BY "email" DESC NULLS LAST`) {
		t.Errorf("SQL() = %s", sql)
	}
}

func TestNullsOrder_PostgresDialectDirectly(t *testing.T) {
	d := postgres.Dialect{}
	first, err := d.RenderNullsOrder(`"a" ASC`, true)
	if err != nil || first != `"a" ASC NULLS FIRST` {
		t.Errorf("RenderNullsOrder(first) = %q, %v", first, err)
	}
	last, err := d.RenderNullsOrder(`"a" DESC`, false)
	if err != nil || last != `"a" DESC NULLS LAST` {
		t.Errorf("RenderNullsOrder(last) = %q, %v", last, err)
	}
}
