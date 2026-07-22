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

// A reading per sensor per day, which is enough for a running total, a rank
// within a group, a neighbour, and the latest row per sensor.
type wReading struct {
	ID      int
	Sensor  string
	Day     int
	Value   int
	Comment *string
}

type wReadingModel struct {
	orm.Table[wReading]
	ID      *orm.IntColumn
	Sensor  *orm.StringColumn
	Day     *orm.IntColumn
	Value   *orm.IntColumn
	Comment *orm.NullableStringColumn
}

var wReadings = orm.DefineTable[wReading]("w_readings",
	func(t *orm.TableBuilder[wReading]) *wReadingModel {
		return &wReadingModel{
			Table:   t.Table(),
			ID:      t.Int("id").PrimaryKey(),
			Sensor:  t.String("sensor").NotNull().MaxLen(10),
			Day:     t.Int("day").NotNull(),
			Value:   t.Int("value").NotNull(),
			Comment: t.NullableString("comment"),
		}
	})

// A table related to nothing, for the outer joins.
type wAlert struct {
	ID        int
	ReadingID int
	Level     string
}

type wAlertModel struct {
	orm.Table[wAlert]
	ID        *orm.IntColumn
	ReadingID *orm.IntColumn
	Level     *orm.StringColumn
}

var wAlerts = orm.DefineTable[wAlert]("w_alerts", func(t *orm.TableBuilder[wAlert]) *wAlertModel {
	return &wAlertModel{
		Table:     t.Table(),
		ID:        t.Int("id").PrimaryKey(),
		ReadingID: t.Int("reading_id").NotNull(),
		Level:     t.String("level").NotNull().MaxLen(10),
	}
})

// The compile tests assert the statement; this asserts the answer. A window
// function is where the two differ most: the SQL parses whatever the frame
// says, and only real rows show whether it counted what it was meant to.
func TestWindows_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS w_readings, w_alerts CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(wReadings, wAlerts)
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

	// Sensor a: 10, 20, 30 over days 1..3. Sensor b: 5, 15 over days 1..2.
	// Comments are missing for two rows, so NULL placement is visible.
	if _, err := conn.Exec(ctx, `
		INSERT INTO w_readings (id, sensor, day, value, comment) OVERRIDING SYSTEM VALUE VALUES
			(1, 'a', 1, 10, 'first'),
			(2, 'a', 2, 20, NULL),
			(3, 'a', 3, 30, 'third'),
			(4, 'b', 1,  5, NULL),
			(5, 'b', 2, 15, 'later')`); err != nil {
		t.Fatalf("seeding readings failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO w_alerts (id, reading_id, level) OVERRIDING SYSTEM VALUE VALUES
			(1, 3, 'high'), (2, 99, 'orphan')`); err != nil {
		t.Fatalf("seeding alerts failed: %v", err)
	}

	db := orm.NewDB(conn, dialect)

	// A running total per sensor, which is the shape windows exist for.
	t.Run("running total per sensor", func(t *testing.T) {
		type row struct {
			Sensor  string
			Day     int
			Running int
		}
		got, err := orm.SelectAs[row](wReadings.With(db),
			wReadings.Sensor, wReadings.Day,
			orm.SumOf(wReadings.Value).
				PartitionBy(wReadings.Sensor).
				OrderBy(wReadings.Day.Asc()).
				Rows(orm.UnboundedPreceding(), orm.CurrentRow()),
		).OrderBy(wReadings.Sensor.Asc(), wReadings.Day.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		want := []int{10, 30, 60, 5, 20}
		if len(got) != len(want) {
			t.Fatalf("All() returned %d rows, want %d", len(got), len(want))
		}
		for i, w := range want {
			if got[i].Running != w {
				t.Errorf("row %d running = %d, want %d", i, got[i].Running, w)
			}
		}
	})

	// A trailing frame counts a fixed number of rows back, which is the
	// case a frame is worth writing for.
	t.Run("trailing two rows", func(t *testing.T) {
		type row struct {
			Day      int
			Trailing int
		}
		got, err := orm.SelectAs[row](
			wReadings.With(db).Where(wReadings.Sensor.Equals("a")),
			wReadings.Day,
			orm.SumOf(wReadings.Value).
				OrderBy(wReadings.Day.Asc()).
				Rows(orm.Preceding(1), orm.CurrentRow()),
		).OrderBy(wReadings.Day.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		want := []int{10, 30, 50} // 10, 10+20, 20+30
		for i, w := range want {
			if got[i].Trailing != w {
				t.Errorf("day %d trailing = %d, want %d", got[i].Day, got[i].Trailing, w)
			}
		}
	})

	// Lag reads the neighbour, and is NULL where there is none.
	t.Run("lag reads the previous row", func(t *testing.T) {
		type row struct {
			Day      int
			Previous *int
		}
		got, err := orm.SelectAs[row](
			wReadings.With(db).Where(wReadings.Sensor.Equals("a")),
			wReadings.Day,
			orm.Lag(wReadings.Value).OrderBy(wReadings.Day.Asc()),
		).OrderBy(wReadings.Day.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if got[0].Previous != nil {
			t.Errorf("first row previous = %d, want none", *got[0].Previous)
		}
		if got[1].Previous == nil || *got[1].Previous != 10 {
			t.Errorf("second row previous = %v, want 10", got[1].Previous)
		}
	})

	// Ranking inside a derived table, then filtering on the rank — the
	// query a window function cannot answer on its own.
	t.Run("top row per sensor", func(t *testing.T) {
		type ranked struct {
			Sensor string
			Value  int
			Rank   int64
		}
		type rankedModel struct {
			orm.DerivedTable[ranked]
			Sensor *orm.StringColumn
			Value  *orm.IntColumn
			Rank   *orm.BigIntColumn
		}
		rankedT := orm.DefineDerived[ranked]("ranked_readings",
			func(t *orm.TableBuilder[ranked]) *rankedModel {
				return &rankedModel{
					DerivedTable: t.Derived(),
					Sensor:       t.String("sensor"),
					Value:        t.Int("value"),
					Rank:         t.BigInt("rank"),
				}
			})

		inner := orm.SelectAs[ranked](wReadings.With(db),
			wReadings.Sensor, wReadings.Value,
			orm.RowNumber().PartitionBy(wReadings.Sensor).OrderBy(wReadings.Value.Desc()),
		)
		got, err := rankedT.From(inner).
			Where(rankedT.Rank.Equals(1)).
			OrderBy(rankedT.Sensor.Asc()).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 2 || got[0].Value != 30 || got[1].Value != 15 {
			t.Errorf("All() = %+v, want the highest reading of each sensor", got)
		}
	})

	// NULLs sort where they are told to, not where Postgres would put them.
	t.Run("nulls placed explicitly", func(t *testing.T) {
		first, err := wReadings.With(db).OrderBy(wReadings.Comment.Asc().NullsFirst()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if first[0].Comment != nil {
			t.Errorf("first row comment = %v, want a NULL first", *first[0].Comment)
		}
		last, err := wReadings.With(db).OrderBy(wReadings.Comment.Asc().NullsLast()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if last[len(last)-1].Comment != nil {
			t.Errorf("last row comment = %v, want a NULL last", *last[len(last)-1].Comment)
		}
	})

	// The latest reading per sensor, in one statement and no subquery.
	t.Run("distinct on keeps the first row per key", func(t *testing.T) {
		got, err := wReadings.With(db).
			DistinctOn(wReadings.Sensor).
			OrderBy(wReadings.Sensor.Asc(), wReadings.Day.Desc()).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("All() returned %d rows, want one per sensor", len(got))
		}
		if got[0].Value != 30 || got[1].Value != 15 {
			t.Errorf("All() = %+v, want the latest reading of each sensor", got)
		}
	})

	// A right join keeps the alert whose reading was deleted, with this
	// table's columns NULL for it.
	t.Run("right join keeps unmatched rows of the joined table", func(t *testing.T) {
		type row struct {
			Level  string
			Sensor *string
		}
		got, err := orm.SelectAs[row](
			wReadings.With(db).RightJoinTo(wAlerts,
				wAlerts.ReadingID.Value().Equals(wReadings.ID)),
			wAlerts.Level, wReadings.Sensor,
		).OrderBy(wAlerts.ID.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("All() returned %d rows, want both alerts", len(got))
		}
		if got[0].Sensor == nil || *got[0].Sensor != "a" {
			t.Errorf("matched alert sensor = %v, want a", got[0].Sensor)
		}
		if got[1].Sensor != nil {
			t.Errorf("orphan alert sensor = %v, want none", *got[1].Sensor)
		}
	})

	// A full join keeps both sides' unmatched rows.
	t.Run("full join keeps both sides", func(t *testing.T) {
		type row struct {
			Level  *string
			Sensor *string
		}
		got, err := orm.SelectAs[row](
			wReadings.With(db).FullJoinTo(wAlerts,
				wAlerts.ReadingID.Value().Equals(wReadings.ID)),
			wAlerts.Level, wReadings.Sensor,
		).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		// Five readings, one of which matches an alert, plus the orphan.
		if len(got) != 6 {
			t.Errorf("All() returned %d rows, want 6", len(got))
		}
	})

	// Reading a right join into *E is refused before the statement runs,
	// since an unmatched row has no row of this table behind it.
	t.Run("a right join cannot be read into the row type", func(t *testing.T) {
		_, err := wReadings.With(db).
			RightJoinTo(wAlerts, wAlerts.ReadingID.Value().Equals(wReadings.ID)).
			All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the read refused")
		}
	})
}
