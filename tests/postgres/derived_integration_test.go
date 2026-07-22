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

type dScore struct {
	ID     int
	Team   string
	Player string
	Points int
}

type dScoreModel struct {
	orm.Table[dScore]
	ID     *orm.IntColumn
	Team   *orm.StringColumn
	Player *orm.StringColumn
	Points *orm.IntColumn
}

var dScores = orm.DefineTable[dScore]("d_scores", func(t *orm.TableBuilder[dScore]) *dScoreModel {
	return &dScoreModel{
		Table:  t.Table(),
		ID:     t.Int("id").PrimaryKey(),
		Team:   t.String("team").NotNull().MaxLen(20),
		Player: t.String("player").NotNull().MaxLen(20),
		Points: t.Int("points").NotNull(),
	}
})

// The derived shape for "each team's top scorers", declared as a model so
// the outer query's columns are typed.
type dRanked struct {
	Team   string
	Player string
	Rank   int64
}

type dRankedModel struct {
	orm.DerivedTable[dRanked]
	Team   *orm.StringColumn
	Player *orm.StringColumn
	Rank   *orm.BigIntColumn
}

var dRankedT = orm.DefineDerived[dRanked]("ranked_scores",
	func(t *orm.TableBuilder[dRanked]) *dRankedModel {
		return &dRankedModel{
			DerivedTable: t.Derived(),
			Team:         t.String("team"),
			Player:       t.String("player"),
			Rank:         t.BigInt("rank"),
		}
	})

// The per-team totals, which the aggregate-of-an-aggregate test groups
// again.
type dTeamTotal struct {
	Team  string
	Total int
}

type dTeamTotalModel struct {
	orm.DerivedTable[dTeamTotal]
	Team  *orm.StringColumn
	Total *orm.IntColumn
}

var dTeamTotals = orm.DefineDerived[dTeamTotal]("team_totals",
	func(t *orm.TableBuilder[dTeamTotal]) *dTeamTotalModel {
		return &dTeamTotalModel{
			DerivedTable: t.Derived(),
			Team:         t.String("team"),
			Total:        t.Int("total"),
		}
	})

// A derived table only earns its keep if the database accepts the statement
// and returns the right rows. The compile tests assert the string; these
// assert the answer.
func TestDerivedTables_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS d_scores CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(dScores)
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

	// Two teams of three. Red totals 60, Blue 33.
	if _, err := conn.Exec(ctx, `
		INSERT INTO d_scores (team, player, points) VALUES
			('red',  'ana',   30), ('red',  'ben',  20), ('red',  'cal',  10),
			('blue', 'dee',   18), ('blue', 'eli',   9), ('blue', 'fay',   6)`); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	db := orm.NewDB(conn, dialect)

	// The motivating case: filtering on a window function, which needs the
	// projection to be a table before a WHERE can name its result.
	t.Run("top two per group", func(t *testing.T) {
		inner := orm.SelectAs[dRanked](
			dScores.With(db),
			dScores.Team, dScores.Player,
			orm.RowNumber().PartitionBy(dScores.Team).OrderBy(dScores.Points.Desc()),
		)
		got, err := dRankedT.From(inner).
			Where(dRankedT.Rank.LessOrEqual(2)).
			OrderBy(dRankedT.Team.Asc(), dRankedT.Rank.Asc()).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 4 {
			t.Fatalf("got %d rows, want 4 — two per team", len(got))
		}
		want := []string{"dee", "eli", "ana", "ben"}
		for i, w := range want {
			if got[i].Player != w {
				t.Errorf("row %d is %s, want %s", i, got[i].Player, w)
			}
		}
		// The third-placed players must be absent, which is the whole point.
		for _, r := range got {
			if r.Player == "cal" || r.Player == "fay" {
				t.Errorf("%s ranked %d should have been filtered out", r.Player, r.Rank)
			}
		}
	})

	// The second case: an aggregate over an aggregate, which needs the
	// grouped result to be a table before it can be aggregated again.
	t.Run("aggregate of an aggregate", func(t *testing.T) {
		perTeam := orm.SelectAs[dTeamTotal](
			dScores.With(db), dScores.Team, orm.SumOf(dScores.Points),
		).GroupBy(dScores.Team)

		mean, err := orm.Avg(ctx, dTeamTotals.From(perTeam), dTeamTotals.Total)
		if err != nil {
			t.Fatalf("Avg() error = %v", err)
		}
		// Red totals 60 and Blue 33, so the mean of the two totals is 46.5.
		if mean != 46.5 {
			t.Errorf("Avg() = %v, want 46.5", mean)
		}
	})

	// Counting a derived table counts its rows, not the underlying table's.
	t.Run("count over a derived table", func(t *testing.T) {
		perTeam := orm.SelectAs[dTeamTotal](
			dScores.With(db), dScores.Team, orm.SumOf(dScores.Points),
		).GroupBy(dScores.Team)

		n, err := dTeamTotals.From(perTeam).Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if n != 2 {
			t.Errorf("Count() = %d, want 2 — one row per team", n)
		}
	})

	// A conditional tally beside a plain one, which is what aggregating an
	// expression brought.
	t.Run("conditional tally", func(t *testing.T) {
		type row struct {
			Team    string
			Doubles int
		}
		got, err := orm.SelectAs[row](
			dScores.With(db), dScores.Team,
			orm.SumOfExpr(orm.Case[int]().When(dScores.Points.GreaterOrEqual(18), 1).Else(0)),
		).GroupBy(dScores.Team).OrderBy(dScores.Team.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		// blue has one score >= 18 (dee, 18); red has two (ana 30, ben 20).
		if len(got) != 2 || got[0].Doubles != 1 || got[1].Doubles != 2 {
			t.Fatalf("got %+v, want blue 1 and red 2", got)
		}
	})
}
