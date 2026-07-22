package query_test

import (
	"context"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// Compiling a read is what every query pays before it reaches the driver, so
// a projection must not cost more than the whole-row read it narrows.
func BenchmarkCompileSelect(b *testing.B) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	b.Run("every column", func(b *testing.B) {
		q := Users.With(db).Where(Users.Age.GreaterThan(18)).OrderBy(Users.ID.Desc()).Limit(20)
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := q.SQL(); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("two columns", func(b *testing.B) {
		q := Users.With(db).Select(Users.ID, Users.Username).
			Where(Users.Age.GreaterThan(18)).OrderBy(Users.ID.Desc()).Limit(20)
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := q.SQL(); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("one column, typed", func(b *testing.B) {
		s := orm.Select(
			Users.With(db).Where(Users.Age.GreaterThan(18)).OrderBy(Users.ID.Desc()).Limit(20),
			Users.Username,
		)
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := s.SQL(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Reading is where a projection should actually pay off: fewer destinations to
// build per row, and for the typed form no row struct at all.
func BenchmarkReadRows(b *testing.B) {
	const rows = 100
	ctx := context.Background()

	b.Run("whole rows", func(b *testing.B) {
		c := fakedriver.NewConn()
		db := orm.NewDB(c, postgres.Dialect{})
		full := make([][]any, rows)
		for i := range full {
			full[i] = row(i, "user", nil, i, nil, stampedAt)
		}
		for range b.N {
			c.QueueRows(full...)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := Users.With(db).All(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("one column into the row type", func(b *testing.B) {
		c := fakedriver.NewConn()
		db := orm.NewDB(c, postgres.Dialect{})
		narrow := make([][]any, rows)
		for i := range narrow {
			narrow[i] = []any{"user"}
		}
		for range b.N {
			c.QueueRows(narrow...)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := Users.With(db).Select(Users.Username).All(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("one column, typed", func(b *testing.B) {
		c := fakedriver.NewConn()
		db := orm.NewDB(c, postgres.Dialect{})
		narrow := make([][]any, rows)
		for i := range narrow {
			narrow[i] = []any{"user"}
		}
		for range b.N {
			c.QueueRows(narrow...)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := orm.Select(Users.With(db), Users.Username).All(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
}
