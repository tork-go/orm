//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type qPrefs struct {
	Theme string `json:"theme"`
}

type qUser struct {
	ID       int
	Username string
	Email    *string
	Age      int
	Prefs    qPrefs
	Joined   time.Time
}

type qUserModel struct {
	orm.Table[qUser]
	ID       *orm.IntColumn
	Username *orm.StringColumn
	Email    *orm.NullableStringColumn
	Age      *orm.IntColumn
	Prefs    *orm.JSONColumn[qPrefs]
	Joined   *orm.TimeColumn
}

var qUsers = orm.DefineTable[qUser]("q_users", func(t *orm.TableBuilder[qUser]) *qUserModel {
	return &qUserModel{
		Table:    t.Table(),
		ID:       t.Int("id").PrimaryKey(),
		Username: t.String("username").NotNull().MaxLen(30),
		Email:    t.NullableString("email"),
		Age:      t.Int("age").NotNull(),
		Prefs:    orm.NewJSONColumn[qPrefs]("prefs"),
		Joined:   t.Time("joined").NotNull(),
	}
})

// The compiler is tested against a fake dialect elsewhere. This runs the
// SQL it produces against the database it was written for, which is the
// only way to learn that Postgres accepts it.
func TestQuery_ReadsAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS q_users CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(qUsers)
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

	joined := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := conn.Exec(ctx, `
		INSERT INTO q_users (username, email, age, prefs, joined) VALUES
			('alice', 'alice@example.com', 30, '{"theme":"dark"}', $1),
			('bob',   NULL,                41, '{}',               $1),
			('carol', 'carol@example.com', 25, '{"theme":"light"}', $1)`, joined); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	db := orm.NewDB(conn, dialect)

	t.Run("all with filter, ordering and paging", func(t *testing.T) {
		got, err := qUsers.With(db).
			Where(qUsers.Age.GreaterThan(20), qUsers.Username.Contains("a")).
			OrderBy(qUsers.Age.Desc()).
			Limit(2).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("All() returned %d rows, want 2", len(got))
		}
		if got[0].Username != "alice" || got[1].Username != "carol" {
			t.Errorf("order = %s, %s, want alice, carol", got[0].Username, got[1].Username)
		}
	})

	t.Run("nullable and document columns survive the trip", func(t *testing.T) {
		alice, err := qUsers.With(db).Where(qUsers.Username.Equals("alice")).First(ctx)
		if err != nil {
			t.Fatalf("First() error = %v", err)
		}
		if alice.Email == nil || *alice.Email != "alice@example.com" {
			t.Errorf("Email = %v, want the address", alice.Email)
		}
		if alice.Prefs.Theme != "dark" {
			t.Errorf("Prefs = %+v, want the decoded document", alice.Prefs)
		}
		if !alice.Joined.Equal(joined) {
			t.Errorf("Joined = %v, want %v", alice.Joined, joined)
		}

		bob, err := qUsers.With(db).Where(qUsers.Username.Equals("bob")).First(ctx)
		if err != nil {
			t.Fatalf("First() error = %v", err)
		}
		if bob.Email != nil {
			t.Errorf("Email = %v, want nil for a NULL", bob.Email)
		}
		// The column is NOT NULL, since its Go type is not a pointer, so
		// an absent document is an empty one rather than a NULL.
		if bob.Prefs.Theme != "" {
			t.Errorf("Prefs = %+v, want the zero value for an empty document", bob.Prefs)
		}
	})

	// The escaping is written in one package and the ESCAPE clause in
	// another; only a real database proves they agree inside a generated
	// query rather than in isolation.
	t.Run("pattern matching escapes wildcards", func(t *testing.T) {
		n, err := qUsers.With(db).Where(qUsers.Username.Contains("a_i")).Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if n != 0 {
			t.Errorf("Contains(%q) matched %d rows, want 0: the underscore must be literal", "a_i", n)
		}
		if n, err = qUsers.With(db).Where(qUsers.Username.StartsWith("ali")).Count(ctx); err != nil || n != 1 {
			t.Errorf("StartsWith(\"ali\") = %d, %v, want 1, nil", n, err)
		}
	})

	t.Run("find by primary key", func(t *testing.T) {
		alice, err := qUsers.With(db).Where(qUsers.Username.Equals("alice")).First(ctx)
		if err != nil {
			t.Fatalf("First() error = %v", err)
		}
		again, err := qUsers.With(db).Find(ctx, alice.ID)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		if again.Username != "alice" {
			t.Errorf("Find() returned %q, want alice", again.Username)
		}
	})

	t.Run("no rows", func(t *testing.T) {
		_, err := qUsers.With(db).Where(qUsers.Username.Equals("nobody")).First(ctx)
		if !errors.Is(err, orm.ErrNoRows) {
			t.Errorf("First() error = %v, want ErrNoRows", err)
		}
		if _, err := qUsers.With(db).Find(ctx, 999999); !errors.Is(err, orm.ErrNoRows) {
			t.Errorf("Find() error = %v, want ErrNoRows", err)
		}
	})

	t.Run("count and exists", func(t *testing.T) {
		n, err := qUsers.With(db).Count(ctx)
		if err != nil || n != 3 {
			t.Errorf("Count() = %d, %v, want 3, nil", n, err)
		}
		ok, err := qUsers.With(db).Where(qUsers.Email.IsNull()).Exists(ctx)
		if err != nil || !ok {
			t.Errorf("Exists() = %v, %v, want true, nil", ok, err)
		}
	})

	// Holding a query and narrowing it differently per branch is the shape
	// this is for. When the branches shared state each carried the other's
	// condition and both matched nothing, which is the kind of wrong that
	// returns an answer rather than an error.
	t.Run("branching a held query", func(t *testing.T) {
		adults := qUsers.With(db).Where(qUsers.Age.GreaterOrEqual(25))

		alice, err := adults.Where(qUsers.Username.Equals("alice")).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		carol, err := adults.Where(qUsers.Username.Equals("carol")).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(alice) != 1 || alice[0].Username != "alice" {
			t.Errorf("the alice branch matched %d rows, want just alice", len(alice))
		}
		if len(carol) != 1 || carol[0].Username != "carol" {
			t.Errorf("the carol branch matched %d rows, want just carol", len(carol))
		}

		// And the query they came from still means what it did.
		all, err := adults.All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		// alice 30, bob 41, carol 25: all three are 25 or older.
		if len(all) != 3 {
			t.Errorf("the base query matched %d rows, want 3: it was narrowed by a branch", len(all))
		}
	})

	// An empty IN compiles to a condition rather than to IN (), which
	// Postgres rejects outright.
	t.Run("empty in list", func(t *testing.T) {
		n, err := qUsers.With(db).Where(qUsers.ID.In()).Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if n != 0 {
			t.Errorf("IN () matched %d rows, want 0", n)
		}
	})
}

// Writing against a real database is the only way to learn that the
// generated statements are accepted, that a generated key really comes
// back, and that a document survives a round trip through the column type
// rather than only through the fake driver.
func TestQuery_WritesAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS q_users CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(qUsers)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	ops, _ := migrate.Diff(schema.Schema{}, desired)
	ddl, err := migrate.Generate(dialect, ops)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := conn.Exec(ctx, ddl); err != nil {
		t.Fatalf("applying schema failed: %v", err)
	}

	db := orm.NewDB(conn, dialect)
	joined := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)

	u := &qUser{
		Username: "alice",
		Email:    ptrTo("alice@example.com"),
		Age:      30,
		Prefs:    qPrefs{Theme: "dark"},
		Joined:   joined,
	}

	t.Run("insert fills the generated key", func(t *testing.T) {
		if err := qUsers.With(db).Insert(ctx, u); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
		if u.ID == 0 {
			t.Fatal("ID is still zero, want the key Postgres generated")
		}
	})

	t.Run("the stored row matches what was written", func(t *testing.T) {
		got, err := qUsers.With(db).Find(ctx, u.ID)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		if got.Username != "alice" || got.Age != 30 {
			t.Errorf("stored row = %+v", got)
		}
		if got.Email == nil || *got.Email != "alice@example.com" {
			t.Errorf("Email = %v, want the address", got.Email)
		}
		if got.Prefs.Theme != "dark" {
			t.Errorf("Prefs = %+v, want the document round tripped", got.Prefs)
		}
		if !got.Joined.Equal(joined) {
			t.Errorf("Joined = %v, want %v", got.Joined, joined)
		}
	})

	t.Run("update writes every column", func(t *testing.T) {
		u.Username = "hasan"
		u.Age = 15
		u.Email = nil
		u.Prefs = qPrefs{Theme: "light"}
		if err := qUsers.With(db).Update(ctx, u); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		got, err := qUsers.With(db).Find(ctx, u.ID)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		if got.Username != "hasan" || got.Age != 15 {
			t.Errorf("stored row = %+v, want the update applied", got)
		}
		if got.Email != nil {
			t.Errorf("Email = %v, want the nil written through", got.Email)
		}
		if got.Prefs.Theme != "light" {
			t.Errorf("Prefs = %+v, want the new document", got.Prefs)
		}
	})

	t.Run("save inserts then updates", func(t *testing.T) {
		fresh := &qUser{Username: "carol", Age: 22, Joined: joined}
		if err := qUsers.With(db).Save(ctx, fresh); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		if fresh.ID == 0 {
			t.Fatal("Save did not insert: the key is still zero")
		}
		first := fresh.ID

		fresh.Age = 23
		if err := qUsers.With(db).Save(ctx, fresh); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		if fresh.ID != first {
			t.Errorf("Save inserted a second row: ID went %d to %d", first, fresh.ID)
		}
		got, err := qUsers.With(db).Find(ctx, first)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		if got.Age != 23 {
			t.Errorf("Age = %d, want the update Save performed", got.Age)
		}
	})

	t.Run("delete removes the row", func(t *testing.T) {
		if err := qUsers.With(db).Delete(ctx, u); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if _, err := qUsers.With(db).Find(ctx, u.ID); !errors.Is(err, orm.ErrNoRows) {
			t.Errorf("Find() error = %v, want ErrNoRows after the delete", err)
		}
	})

	// A write that matched nothing is reported rather than passed off as
	// success, which is the whole reason the row count is read back.
	t.Run("writing a row that is not there", func(t *testing.T) {
		gone := &qUser{ID: 999999, Username: "x", Joined: joined}
		if err := qUsers.With(db).Update(ctx, gone); !errors.Is(err, orm.ErrNoRows) {
			t.Errorf("Update() error = %v, want ErrNoRows", err)
		}
		if err := qUsers.With(db).Delete(ctx, gone); !errors.Is(err, orm.ErrNoRows) {
			t.Errorf("Delete() error = %v, want ErrNoRows", err)
		}
	})
}

func ptrTo[T any](v T) *T { return &v }
