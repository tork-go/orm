package query_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// The batch sizes every size-sensitive benchmark runs at. One row is the
// case a batch must not make slower than the single row path; a thousand is
// where the cost of building one statement per row would show.
var batchSizes = []int{1, 10, 100, 1000}

// Memberships needs nothing read back, so this measures statement building
// and value binding without a queued result set in the way.
func BenchmarkInsertMany(b *testing.B) {
	for _, size := range batchSizes {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			rows := make([]*Membership, size)
			for i := range rows {
				rows[i] = &Membership{OrgID: 1, UserID: i}
			}
			ctx := context.Background()
			db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

			b.ReportAllocs()
			for b.Loop() {
				if err := Memberships.With(db).InsertMany(ctx, rows...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Rows that write different columns are grouped before any statement is
// built, so this is the cost of that grouping on top of the building.
func BenchmarkInsertMany_MixedColumns(b *testing.B) {
	const size = 100

	rows := make([]*noted, size)
	for i := range rows {
		rows[i] = &noted{Key: fmt.Sprint(i), Name: "n"}
		if i%2 == 1 {
			rows[i].Note = "supplied"
		}
	}

	c := fakedriver.NewConn()
	d := fakedriver.NewDialect()
	d.CanReturn = true
	db := orm.NewDB(c, d)
	ctx := context.Background()

	// The rows leaving note to the database read it back, one statement's
	// worth per iteration. Queued up front so the loop measures only the
	// write.
	back := make([][]any, size/2)
	for i := range back {
		back[i] = []any{"default"}
	}
	for range b.N {
		c.QueueRows(back...)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := Noted.With(db).InsertMany(ctx, rows...); err != nil {
			b.Fatal(err)
		}
	}
}

// One statement per row, so this is where the batch's own overhead over a
// loop of Update calls would show.
func BenchmarkUpdateMany(b *testing.B) {
	for _, size := range batchSizes {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			rows := make([]*User, size)
			for i := range rows {
				rows[i] = &User{ID: i + 1, Username: "u", Age: i}
			}
			c := fakedriver.NewConn()
			c.RowsAffected = 1
			db := orm.NewDB(c, postgres.Dialect{})
			ctx := context.Background()

			b.ReportAllocs()
			for b.Loop() {
				if _, err := Users.With(db).UpdateMany(ctx, rows...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// A single column key becomes one IN list however many rows there are.
func BenchmarkDeleteMany(b *testing.B) {
	for _, size := range batchSizes {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			rows := make([]*User, size)
			for i := range rows {
				rows[i] = &User{ID: i + 1}
			}
			c := fakedriver.NewConn()
			c.RowsAffected = int64(size)
			db := orm.NewDB(c, postgres.Dialect{})
			ctx := context.Background()

			b.ReportAllocs()
			for b.Loop() {
				if _, err := Users.With(db).DeleteMany(ctx, rows...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// A composite key has no list form, so it builds a comparison per row and
// per key column. Worth measuring apart from the list case.
func BenchmarkDeleteMany_CompositeKey(b *testing.B) {
	for _, size := range batchSizes {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			rows := make([]*Membership, size)
			for i := range rows {
				rows[i] = &Membership{OrgID: 1, UserID: i}
			}
			c := fakedriver.NewConn()
			c.RowsAffected = int64(size)
			db := orm.NewDB(c, postgres.Dialect{})
			ctx := context.Background()

			b.ReportAllocs()
			for b.Loop() {
				if _, err := Memberships.With(db).DeleteMany(ctx, rows...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// A set operation is one statement whatever it matches, so its cost is
// compiling the assignments and the filter.
func BenchmarkUpdateAll(b *testing.B) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		_, err := Users.With(db).
			Where(Users.Age.Lt(18), Users.Email.IsNotNull()).
			UpdateAll(ctx, Users.Username.Set("minor"), Users.Email.SetNull())
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDeleteAll(b *testing.B) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := Users.With(db).Where(Users.Age.Lt(18)).DeleteAll(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// The wrapper itself, apart from anything it runs, since every batch of more
// than one statement pays for it.
func BenchmarkTransaction(b *testing.B) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if err := db.Transaction(ctx, func(tx *orm.DB) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}

// noted has a string primary key, so it is not an identity column, and a
// column the database fills in, so rows differ in what they write. That is
// what the grouping benchmark needs and no other fixture provides.
type noted struct {
	Key  string
	Name string
	Note string
}

type notedModel struct {
	orm.Table[noted]
	Key  *orm.StringColumn
	Name *orm.StringColumn
	Note *orm.StringColumn
}

var Noted = orm.DefineTable[noted]("noted", func(t *orm.TableBuilder[noted]) *notedModel {
	return &notedModel{
		Table: t.Table(),
		Key:   t.String("key").PrimaryKey(),
		Name:  t.String("name").NotNull(),
		Note:  t.String("note").NotNull().ServerDefault("'none'"),
	}
})
