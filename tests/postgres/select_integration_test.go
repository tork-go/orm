//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type sPrefs struct {
	Theme string `json:"theme"`
}

type sUser struct {
	ID      int
	Name    string
	Country string
	Email   *string
	Age     int
	Prefs   sPrefs
}

type sUserModel struct {
	orm.Table[sUser]
	ID      *orm.IntColumn
	Name    *orm.StringColumn
	Country *orm.StringColumn
	Email   *orm.NullableStringColumn
	Age     *orm.IntColumn
	Prefs   *orm.JSONColumn[sPrefs]
}

var sUsers = orm.DefineTable[sUser]("s_users", func(t *orm.TableBuilder[sUser]) *sUserModel {
	return &sUserModel{
		Table:   t.Table(),
		ID:      t.Int("id").PrimaryKey(),
		Name:    t.String("name").NotNull(),
		Country: t.String("country").NotNull(),
		Email:   t.NullableString("email"),
		Age:     t.Int("age").NotNull(),
		Prefs:   orm.NewJSONColumn[sPrefs]("prefs"),
	}
})

// The statements a projection builds are checked against a fake dialect
// elsewhere. This runs them against the database they were written for, which
// is the only way to learn that Postgres accepts them — the derived table a
// distinct count wraps itself in especially, since Postgres rejects one
// without a name.
func TestSelect_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS s_users CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(sUsers)
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

	db := orm.NewDB(conn, dialect)
	email := "alice@example.com"
	seed := []*sUser{
		{Name: "alice", Country: "TR", Email: &email, Age: 30, Prefs: sPrefs{Theme: "dark"}},
		{Name: "bob", Country: "TR", Age: 41},
		{Name: "carol", Country: "DE", Age: 25, Prefs: sPrefs{Theme: "light"}},
		{Name: "dave", Country: "DE", Age: 25},
	}
	if err := sUsers.With(db).InsertMany(ctx, seed...); err != nil {
		t.Fatalf("InsertMany failed: %v", err)
	}

	t.Run("a projection reads only what it asked for", func(t *testing.T) {
		got, err := sUsers.With(db).
			Select(sUsers.ID, sUsers.Name).
			Where(sUsers.Name.Equals("alice")).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("All() returned %d rows, want 1", len(got))
		}
		if got[0].Name != "alice" || got[0].ID == 0 {
			t.Errorf("read %+v, want the selected columns", got[0])
		}
		// Everything unselected is zero, including a column that genuinely
		// holds a value in the database.
		if got[0].Age != 0 || got[0].Email != nil || got[0].Country != "" {
			t.Errorf("read %+v, want everything unselected left zero", got[0])
		}
	})

	t.Run("distinct over a projection", func(t *testing.T) {
		got, err := sUsers.With(db).Select(sUsers.Country).Distinct().
			OrderBy(sUsers.Country.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 2 || got[0].Country != "DE" || got[1].Country != "TR" {
			t.Errorf("All() = %+v, want one row per country", got)
		}
	})

	// The derived table is the part Postgres is fussy about: it rejects one
	// without an alias, so this proves the alias is there and accepted.
	t.Run("counting a distinct query", func(t *testing.T) {
		n, err := sUsers.With(db).Select(sUsers.Country).Distinct().Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if n != 2 {
			t.Errorf("Count() = %d, want 2 countries", n)
		}

		// Two columns, so the pairs rather than the countries are counted.
		n, err = sUsers.With(db).Select(sUsers.Country, sUsers.Age).Distinct().Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if n != 3 {
			t.Errorf("Count() = %d, want 3 country/age pairs", n)
		}
	})

	t.Run("one column, typed", func(t *testing.T) {
		names, err := orm.Select(sUsers.With(db).OrderBy(sUsers.Name.Asc()), sUsers.Name).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		want := []string{"alice", "bob", "carol", "dave"}
		if len(names) != len(want) {
			t.Fatalf("All() = %v, want %v", names, want)
		}
		for i := range want {
			if names[i] != want[i] {
				t.Errorf("All() = %v, want %v", names, want)
				break
			}
		}
	})

	t.Run("a nullable column comes back as pointers", func(t *testing.T) {
		emails, err := orm.Select(sUsers.With(db).OrderBy(sUsers.Name.Asc()), sUsers.Email).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(emails) != 4 {
			t.Fatalf("All() returned %d values, want 4", len(emails))
		}
		if emails[0] == nil || *emails[0] != email {
			t.Errorf("alice's email = %v, want the address", emails[0])
		}
		for i, e := range emails[1:] {
			if e != nil {
				t.Errorf("row %d = %v, want nil for the NULL", i+1, e)
			}
		}
	})

	t.Run("counting one column", func(t *testing.T) {
		// COUNT of a column does not count NULLs, which is the difference
		// between asking how many values there are and how many rows.
		n, err := orm.Select(sUsers.With(db), sUsers.Email).Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if n != 1 {
			t.Errorf("Count() = %d, want 1: three emails are NULL", n)
		}

		n, err = orm.Select(sUsers.With(db), sUsers.Country).Distinct().Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if n != 2 {
			t.Errorf("Count() = %d, want 2 distinct countries", n)
		}
	})

	t.Run("a document column decodes", func(t *testing.T) {
		prefs, err := orm.Select(sUsers.With(db).OrderBy(sUsers.Name.Asc()), sUsers.Prefs).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(prefs) != 4 || prefs[0].Theme != "dark" || prefs[2].Theme != "light" {
			t.Errorf("All() = %+v, want the documents decoded", prefs)
		}
	})

	// Postgres widens as it aggregates: SUM over an integer column comes back
	// as a bigint, and AVG as a numeric. Whether those land in the Go type the
	// column declares is only answerable here.
	t.Run("aggregates", func(t *testing.T) {
		total, err := orm.Sum(ctx, sUsers.With(db), sUsers.Age)
		if err != nil {
			t.Fatalf("Sum() error = %v", err)
		}
		if total != 30+41+25+25 {
			t.Errorf("Sum() = %d, want 121", total)
		}

		mean, err := orm.Avg(ctx, sUsers.With(db), sUsers.Age)
		if err != nil {
			t.Fatalf("Avg() error = %v", err)
		}
		if mean < 30.2 || mean > 30.3 {
			t.Errorf("Avg() = %v, want about 30.25", mean)
		}

		youngest, err := orm.Min(ctx, sUsers.With(db), sUsers.Age)
		if err != nil {
			t.Fatalf("Min() error = %v", err)
		}
		if youngest != 25 {
			t.Errorf("Min() = %d, want 25", youngest)
		}

		last, err := orm.Max(ctx, sUsers.With(db), sUsers.Name)
		if err != nil {
			t.Fatalf("Max() error = %v", err)
		}
		if last != "dave" {
			t.Errorf("Max() = %q, want dave", last)
		}
	})

	t.Run("aggregates carry the filter and honour distinct", func(t *testing.T) {
		total, err := orm.Sum(ctx, sUsers.With(db).Where(sUsers.Country.Equals("DE")), sUsers.Age)
		if err != nil {
			t.Fatalf("Sum() error = %v", err)
		}
		if total != 50 {
			t.Errorf("Sum() = %d, want 50", total)
		}

		// Both German users are 25, so summing each distinct age once halves it.
		distinct, err := orm.Sum(ctx,
			sUsers.With(db).Where(sUsers.Country.Equals("DE")).Distinct(), sUsers.Age)
		if err != nil {
			t.Fatalf("Sum() error = %v", err)
		}
		if distinct != 25 {
			t.Errorf("Sum() over distinct ages = %d, want 25", distinct)
		}
	})

	t.Run("aggregates over no rows", func(t *testing.T) {
		none := sUsers.With(db).Where(sUsers.Name.Equals("nobody"))

		total, err := orm.Sum(ctx, none, sUsers.Age)
		if err != nil {
			t.Fatalf("Sum() error = %v", err)
		}
		if total != 0 {
			t.Errorf("Sum() = %d, want 0 for an empty set", total)
		}

		if _, err := orm.Min(ctx, none, sUsers.Age); !errors.Is(err, orm.ErrNoRows) {
			t.Errorf("Min() error = %v, want ErrNoRows", err)
		}
		if _, err := orm.Max(ctx, none, sUsers.Age); !errors.Is(err, orm.ErrNoRows) {
			t.Errorf("Max() error = %v, want ErrNoRows", err)
		}
		if _, err := orm.Avg(ctx, none, sUsers.Age); !errors.Is(err, orm.ErrNoRows) {
			t.Errorf("Avg() error = %v, want ErrNoRows", err)
		}
	})

	// A nullable column is a Column[*T], so its aggregate is a *T, and the
	// NULLs three of the four rows hold are skipped rather than counted.
	t.Run("aggregates over a nullable column", func(t *testing.T) {
		largest, err := orm.Max(ctx, sUsers.With(db), sUsers.Email)
		if err != nil {
			t.Fatalf("Max() error = %v", err)
		}
		if largest == nil || *largest != email {
			t.Errorf("Max() = %v, want the one address", largest)
		}

		nothing, err := orm.Max(ctx,
			sUsers.With(db).Where(sUsers.Name.Equals("bob")), sUsers.Email)
		if err != nil {
			t.Fatalf("Max() error = %v", err)
		}
		if nothing != nil {
			t.Errorf("Max() = %v, want nil where every value is NULL", nothing)
		}
	})

	// Grouping is where a database is fussiest: what may appear beside an
	// aggregate, where HAVING sits relative to ORDER BY, and whether NULLs
	// collect into a group of their own.
	t.Run("grouping", func(t *testing.T) {
		byCountry, err := orm.CountBy(sUsers.With(db), sUsers.Country).
			OrderBy(sUsers.Country.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("CountBy() error = %v", err)
		}
		if len(byCountry) != 2 {
			t.Fatalf("got %d buckets, want 2: %+v", len(byCountry), byCountry)
		}
		if byCountry[0].Key != "DE" || byCountry[0].Value != 2 {
			t.Errorf("first = %+v, want {DE 2}", byCountry[0])
		}
		if byCountry[1].Key != "TR" || byCountry[1].Value != 2 {
			t.Errorf("second = %+v, want {TR 2}", byCountry[1])
		}

		sums, err := orm.SumBy(sUsers.With(db), sUsers.Country, sUsers.Age).
			OrderByValueDesc().All(ctx)
		if err != nil {
			t.Fatalf("SumBy() error = %v", err)
		}
		// TR is 30+41, DE is 25+25, so the larger comes first.
		if sums[0].Key != "TR" || sums[0].Value != 71 {
			t.Errorf("first = %+v, want {TR 71}", sums[0])
		}
		if sums[1].Key != "DE" || sums[1].Value != 50 {
			t.Errorf("second = %+v, want {DE 50}", sums[1])
		}

		means, err := orm.AvgBy(sUsers.With(db), sUsers.Country, sUsers.Age).
			OrderBy(sUsers.Country.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("AvgBy() error = %v", err)
		}
		if means[0].Value != 25 {
			t.Errorf("DE mean = %v, want 25", means[0].Value)
		}
	})

	t.Run("having, which the database applies after grouping", func(t *testing.T) {
		// Both countries have two users, so a threshold of three excludes
		// everything and one of two keeps both.
		none, err := orm.CountBy(sUsers.With(db), sUsers.Country).Having(orm.OpGreaterOrEqual, 3).All(ctx)
		if err != nil {
			t.Fatalf("CountBy() error = %v", err)
		}
		if len(none) != 0 {
			t.Errorf("got %+v, want no country with three users", none)
		}

		both, err := orm.CountBy(sUsers.With(db), sUsers.Country).Having(orm.OpGreaterOrEqual, 2).All(ctx)
		if err != nil {
			t.Fatalf("CountBy() error = %v", err)
		}
		if len(both) != 2 {
			t.Errorf("got %d buckets, want 2", len(both))
		}

		// Two conditions bracket the count from both sides.
		between, err := orm.CountBy(sUsers.With(db), sUsers.Country).
			Having(orm.OpGreaterOrEqual, 2).Having(orm.OpLessThan, 3).All(ctx)
		if err != nil {
			t.Fatalf("CountBy() error = %v", err)
		}
		if len(between) != 2 {
			t.Errorf("got %d buckets, want 2", len(between))
		}
	})

	t.Run("having with a filter, ordering and a cap together", func(t *testing.T) {
		got, err := orm.SumBy(sUsers.With(db).Where(sUsers.Age.GreaterOrEqual(25)), sUsers.Country, sUsers.Age).
			Having(orm.OpGreaterThan, 10).
			OrderByValueDesc().
			Limit(1).
			All(ctx)
		if err != nil {
			t.Fatalf("SumBy() error = %v", err)
		}
		if len(got) != 1 || got[0].Key != "TR" {
			t.Errorf("got %+v, want just TR", got)
		}
	})

	// A nullable key collects its NULLs into a group of their own, which K
	// holds because it is a pointer.
	t.Run("grouping by a nullable column", func(t *testing.T) {
		got, err := orm.CountBy(sUsers.With(db), sUsers.Email).OrderByValueDesc().All(ctx)
		if err != nil {
			t.Fatalf("CountBy() error = %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d buckets, want 2: %+v", len(got), got)
		}
		if got[0].Key != nil || got[0].Value != 3 {
			t.Errorf("largest = %+v, want the three NULLs grouped together", got[0])
		}
		if got[1].Key == nil || *got[1].Key != email {
			t.Errorf("second = %+v, want the one address", got[1])
		}
	})

	t.Run("keys alone", func(t *testing.T) {
		keys, err := orm.CountBy(sUsers.With(db), sUsers.Country).
			Having(orm.OpGreaterOrEqual, 2).OrderBy(sUsers.Country.Asc()).Keys(ctx)
		if err != nil {
			t.Fatalf("Keys() error = %v", err)
		}
		if len(keys) != 2 || keys[0] != "DE" || keys[1] != "TR" {
			t.Errorf("Keys() = %v, want [DE TR]", keys)
		}
	})

	t.Run("distinct values of one column", func(t *testing.T) {
		countries, err := orm.Select(sUsers.With(db).OrderBy(sUsers.Country.Asc()), sUsers.Country).
			Distinct().All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(countries) != 2 || countries[0] != "DE" || countries[1] != "TR" {
			t.Errorf("All() = %v, want [DE TR]", countries)
		}
	})
}
