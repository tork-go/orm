//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

// Email is unique, which is what gives an upsert something to conflict on:
// the primary key is generated, so a row identified by one is new by
// construction and could never collide.
type uUser struct {
	ID    int
	Email string
	Name  string
	Age   int
}

type uUserModel struct {
	orm.Table[uUser]
	ID    *orm.IntColumn
	Email *orm.StringColumn
	Name  *orm.StringColumn
	Age   *orm.IntColumn
}

var uUsers = orm.DefineTable[uUser]("u_users", func(t *orm.TableBuilder[uUser]) *uUserModel {
	return &uUserModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Email: t.String("email").Unique().NotNull(),
		Name:  t.String("name").NotNull(),
		Age:   t.Int("age").NotNull(),
	}
})

// The clause an upsert builds is checked against a fake dialect elsewhere.
// This runs it against the database it was written for, which is the only
// place the behaviour can be observed rather than asserted: that DO NOTHING
// really does leave the stored row alone and return nothing for it, that
// EXCLUDED holds the values the insert proposed, and that a batch of both
// kinds ends up with the row count anyone would predict.
func TestUpsert_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS u_users CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(uUsers)
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

	seed := &uUser{Email: "alice@example.com", Name: "alice", Age: 30}
	if err := uUsers.With(db).Insert(ctx, seed); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	stored := func(t *testing.T, email string) *uUser {
		t.Helper()
		got, err := uUsers.With(db).Where(uUsers.Email.Eq(email)).First(ctx)
		if err != nil {
			t.Fatalf("First() error = %v", err)
		}
		return got
	}

	t.Run("do nothing leaves the stored row alone", func(t *testing.T) {
		clash := &uUser{Email: "alice@example.com", Name: "impostor", Age: 1}
		n, err := uUsers.With(db).OnConflict(uUsers.Email).DoNothing().Insert(ctx, clash)
		if err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
		if n != 0 {
			t.Errorf("Insert() = %d, want 0 for a row already there", n)
		}
		if clash.ID != 0 {
			t.Errorf("id = %d, want the skipped row left as it arrived", clash.ID)
		}
		if got := stored(t, "alice@example.com"); got.Name != "alice" || got.Age != 30 {
			t.Errorf("stored row is %+v, want it untouched", got)
		}
	})

	t.Run("do nothing still writes a row that does not clash", func(t *testing.T) {
		fresh := &uUser{Email: "bob@example.com", Name: "bob", Age: 41}
		n, err := uUsers.With(db).OnConflict(uUsers.Email).DoNothing().Insert(ctx, fresh)
		if err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
		if n != 1 {
			t.Errorf("Insert() = %d, want 1", n)
		}
		if fresh.ID == 0 {
			t.Error("the generated key did not come back")
		}
	})

	t.Run("do nothing with no target covers every constraint", func(t *testing.T) {
		n, err := uUsers.With(db).OnConflict().DoNothing().
			Insert(ctx, &uUser{Email: "alice@example.com", Name: "x", Age: 1})
		if err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
		if n != 0 {
			t.Errorf("Insert() = %d, want 0", n)
		}
	})

	t.Run("do update overwrites the named columns only", func(t *testing.T) {
		n, err := uUsers.With(db).OnConflict(uUsers.Email).DoUpdate(uUsers.Age).
			Insert(ctx, &uUser{Email: "alice@example.com", Name: "ignored", Age: 31})
		if err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
		if n != 1 {
			t.Errorf("Insert() = %d, want 1: an overwrite writes a row", n)
		}
		got := stored(t, "alice@example.com")
		if got.Age != 31 {
			t.Errorf("age = %d, want the value the insert proposed", got.Age)
		}
		if got.Name != "alice" {
			t.Errorf("name = %q, want the column that was not named left alone", got.Name)
		}
	})

	t.Run("do update all overwrites everything the insert wrote", func(t *testing.T) {
		row := &uUser{Email: "alice@example.com", Name: "alice cooper", Age: 77}
		n, err := uUsers.With(db).OnConflict(uUsers.Email).DoUpdateAll().Insert(ctx, row)
		if err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
		if n != 1 {
			t.Errorf("Insert() = %d, want 1", n)
		}
		// The overwrite is of the row already there, so the key that comes
		// back is that row's rather than a new one.
		if row.ID != seed.ID {
			t.Errorf("id = %d, want the stored row's %d", row.ID, seed.ID)
		}
		got := stored(t, "alice@example.com")
		if got.Name != "alice cooper" || got.Age != 77 {
			t.Errorf("stored row is %+v, want both columns overwritten", got)
		}
	})

	t.Run("a batch of new and clashing rows", func(t *testing.T) {
		rows := []*uUser{
			{Email: "alice@example.com", Name: "skip me", Age: 1},
			{Email: "carol@example.com", Name: "carol", Age: 25},
			{Email: "bob@example.com", Name: "skip me too", Age: 2},
			{Email: "dave@example.com", Name: "dave", Age: 26},
		}
		n, err := uUsers.With(db).OnConflict(uUsers.Email).DoNothing().InsertMany(ctx, rows...)
		if err != nil {
			t.Fatalf("InsertMany() error = %v", err)
		}
		if n != 2 {
			t.Fatalf("InsertMany() = %d, want the two rows that were new", n)
		}
		// Each row learns its own fate: the new ones carry a key, the skipped
		// ones do not, which is what one row per statement buys.
		for i, want := range []bool{false, true, false, true} {
			if got := rows[i].ID != 0; got != want {
				t.Errorf("row %d has a key: %v, want %v", i, got, want)
			}
		}
		if got := stored(t, "alice@example.com"); got.Name != "alice cooper" {
			t.Errorf("stored row is %+v, want the batch to have skipped it", got)
		}

		total, err := uUsers.With(db).Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if total != 4 {
			t.Errorf("Count() = %d, want alice, bob, carol and dave", total)
		}
	})

	// The whole point of an upsert over a loop: the rows go in once, whichever
	// of them were already there.
	t.Run("the same batch twice writes nothing the second time", func(t *testing.T) {
		batch := func() []*uUser {
			out := make([]*uUser, 3)
			for i := range out {
				out[i] = &uUser{
					Email: fmt.Sprintf("repeat-%d@example.com", i),
					Name:  fmt.Sprintf("repeat-%d", i),
					Age:   i,
				}
			}
			return out
		}
		first, err := uUsers.With(db).OnConflict(uUsers.Email).DoNothing().
			InsertMany(ctx, batch()...)
		if err != nil {
			t.Fatalf("InsertMany() error = %v", err)
		}
		second, err := uUsers.With(db).OnConflict(uUsers.Email).DoNothing().
			InsertMany(ctx, batch()...)
		if err != nil {
			t.Fatalf("InsertMany() error = %v", err)
		}
		if first != 3 || second != 0 {
			t.Errorf("wrote %d then %d, want 3 then 0", first, second)
		}
	})

	// Postgres is the one requiring a target for DO UPDATE, and it says so
	// through the dialect rather than through a statement it would reject.
	t.Run("do update needs a target", func(t *testing.T) {
		_, err := uUsers.With(db).OnConflict().DoUpdateAll().
			Insert(ctx, &uUser{Email: "x@example.com", Name: "x", Age: 1})
		if err == nil {
			t.Fatal("Insert() error = nil, want the missing target reported")
		}
	})
}
