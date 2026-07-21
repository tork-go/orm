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
			Where(qUsers.Age.Gt(20), qUsers.Username.Contains("a")).
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
		alice, err := qUsers.With(db).Where(qUsers.Username.Eq("alice")).First(ctx)
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

		bob, err := qUsers.With(db).Where(qUsers.Username.Eq("bob")).First(ctx)
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
		alice, err := qUsers.With(db).Where(qUsers.Username.Eq("alice")).First(ctx)
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
		_, err := qUsers.With(db).Where(qUsers.Username.Eq("nobody")).First(ctx)
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
