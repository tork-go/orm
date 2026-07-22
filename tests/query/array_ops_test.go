package query_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

type arrRow struct {
	ID   int
	Tags []string
	Nums []int
	Opt  *[]string
}

type arrRowModel struct {
	orm.Table[arrRow]
	ID   *orm.IntColumn
	Tags *orm.StringArrayColumn
	Nums *orm.IntArrayColumn
	Opt  *orm.NullableStringArrayColumn
}

var arrRows = orm.DefineTable[arrRow]("arr_rows", func(t *orm.TableBuilder[arrRow]) *arrRowModel {
	return &arrRowModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Tags:  t.StringArray("tags").NotNull(),
		Nums:  t.IntArray("nums").NotNull(),
		Opt:   t.NullableStringArray("opt"),
	}
})

// oneArg asserts a statement bound exactly one argument, equal to want. The
// membership operators bind the whole slice as one array parameter rather than
// an element per placeholder.
func oneArg(t *testing.T, args []any, want any) {
	t.Helper()
	if len(args) != 1 {
		t.Fatalf("bound %v, want one array argument", args)
	}
	if !reflect.DeepEqual(args[0], want) {
		t.Errorf("bound %#v, want %#v", args[0], want)
	}
}

func TestArray_Has(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		sql, args, err := arrRows.With(db).Where(arrRows.Tags.Has("go")).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE "tags" @> $1`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
		oneArg(t, args, []string{"go"})
	})

	t.Run("fake", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), fakedriver.NewDialect())
		sql, _, err := arrRows.With(db).Where(arrRows.Tags.Has("go")).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE SUPERSET([tags], ?)`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
	})
}

func TestArray_HasAll(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	sql, args, err := arrRows.With(db).Where(arrRows.Tags.HasAll("go", "sql")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE "tags" @> $1`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
	oneArg(t, args, []string{"go", "sql"})
}

func TestArray_HasAny(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	sql, args, err := arrRows.With(db).Where(arrRows.Tags.HasAny("go", "sql")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE "tags" && $1`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
	oneArg(t, args, []string{"go", "sql"})
}

func TestArray_Len(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		sql, args, err := arrRows.With(db).Where(arrRows.Tags.Len().Gt(3)).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE cardinality("tags") > $1`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
		if len(args) != 1 || args[0] != 3 {
			t.Errorf("bound %v, want [3]", args)
		}
	})

	t.Run("every comparison", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		cases := map[string]struct {
			pred orm.Predicate
			op   string
		}{
			"Eq":    {arrRows.Nums.Len().Eq(0), "="},
			"NotEq": {arrRows.Nums.Len().NotEq(0), "<>"},
			"Gt":    {arrRows.Nums.Len().Gt(1), ">"},
			"Gte":   {arrRows.Nums.Len().Gte(1), ">="},
			"Lt":    {arrRows.Nums.Len().Lt(9), "<"},
			"Lte":   {arrRows.Nums.Len().Lte(9), "<="},
		}
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				sql, _, err := arrRows.With(db).Where(tc.pred).SQL()
				if err != nil {
					t.Fatalf("SQL() error = %v", err)
				}
				if want := `cardinality("nums") ` + tc.op + ` $1`; !strings.HasSuffix(sql, want) {
					t.Errorf("compiled %s, want it to end %s", sql, want)
				}
			})
		}
	})

	t.Run("fake", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), fakedriver.NewDialect())
		sql, _, err := arrRows.With(db).Where(arrRows.Tags.Len().Gt(3)).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE SIZE([tags]) > ?`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
	})
}

// The element type is the array's, so an int array takes an int; the slice is
// bound as int[], which is what the whole-array binding is for.
func TestArray_IntElements(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	sql, args, err := arrRows.With(db).Where(arrRows.Nums.Has(3)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE "nums" @> $1`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
	oneArg(t, args, []int{3})
}

// An empty list is defined, the same way an empty IN list is: HasAll of
// nothing is true of every array, so it drops out of the WHERE, and HasAny of
// nothing matches no array.
func TestArray_EmptyLists(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	t.Run("HasAll of nothing matches everything", func(t *testing.T) {
		sql, args, err := arrRows.With(db).Where(arrRows.Tags.HasAll()).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if strings.Contains(sql, "WHERE") {
			t.Errorf("compiled %s, want no WHERE for a condition true of every row", sql)
		}
		if len(args) != 0 {
			t.Errorf("bound %v, want nothing for an empty list answered at compile time", args)
		}
	})

	t.Run("HasAny of nothing matches nothing", func(t *testing.T) {
		sql, _, err := arrRows.With(db).Where(arrRows.Tags.HasAny()).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE (1 = 0)`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
	})
}

// The nullable array column carries the same operations.
func TestArray_NullableColumn(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	sql, _, err := arrRows.With(db).Where(
		arrRows.Opt.Has("go"),
		arrRows.Opt.Len().Gte(1),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE ("opt" @> $1 AND cardinality("opt") >= $2)`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
}

// The array operations number in with the statement's other placeholders.
func TestArray_NumbersWithOtherPredicates(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	sql, args, err := arrRows.With(db).Where(
		arrRows.ID.Gt(10),
		arrRows.Tags.HasAny("go", "sql"),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE ("id" > $1 AND "tags" && $2)`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
	if len(args) != 2 || args[0] != 10 || !reflect.DeepEqual(args[1], []string{"go", "sql"}) {
		t.Errorf("bound %v, want [10 [go sql]]", args)
	}
}

// Naming another table's array column is caught by the compiler, for each of
// the three renderers.
func TestArray_ForeignColumnRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	tests := map[string]orm.Predicate{
		"Has":    arrRows.Tags.Has("go"),
		"HasAny": arrRows.Tags.HasAny("go"),
		"Len":    arrRows.Tags.Len().Gt(1),
	}
	for name, pred := range tests {
		t.Run(name, func(t *testing.T) {
			// The column belongs to arr_rows, not users.
			_, _, err := Users.With(db).Where(pred).SQL()
			if err == nil || !strings.Contains(err.Error(), `belongs to table "arr_rows"`) {
				t.Errorf("SQL() error = %v, want the foreign column rejected", err)
			}
		})
	}
}

// A dialect with no array type says so, naming the operation.
func TestArray_UnsupportedByTheDialect(t *testing.T) {
	d := fakedriver.NewDialect()
	d.NoArray = true
	db := orm.NewDB(fakedriver.NewConn(), d)

	tests := map[string]orm.Predicate{
		"Has":    arrRows.Tags.Has("go"),
		"HasAll": arrRows.Tags.HasAll("go", "sql"),
		"HasAny": arrRows.Tags.HasAny("go", "sql"),
		"Len":    arrRows.Tags.Len().Gt(3),
	}
	for name, pred := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := arrRows.With(db).Where(pred).SQL()
			if err == nil || !strings.Contains(err.Error(), "array") {
				t.Errorf("SQL() error = %v, want it to name the unsupported operation", err)
			}
		})
	}
}
