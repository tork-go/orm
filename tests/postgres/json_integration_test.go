//go:build integration

package postgres_test

import (
	"context"
	"sort"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type jProfile struct {
	Theme    string `json:"theme"`
	Nickname string `json:"nickname,omitempty"`
}

type jUser struct {
	ID    int
	Name  string
	Prefs jProfile
}

type jUserModel struct {
	orm.Table[jUser]
	ID    *orm.IntColumn
	Name  *orm.StringColumn
	Prefs *orm.JSONColumn[jProfile]
}

var jUsers = orm.DefineTable[jUser]("j_users", func(t *orm.TableBuilder[jUser]) *jUserModel {
	return &jUserModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
		Prefs: orm.NewJSONColumn[jProfile]("prefs"),
	}
})

// What the JSON operators render is checked against the fakes. That the SQL
// they render actually runs — the ? key-existence operator not colliding with
// pgx's parameters, the @> containment reading a text value cast to jsonb, the
// ->> extraction comparing as text — is only knowable against a real jsonb
// column, which is what this is for.
func TestJSON_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS j_users CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(jUsers)
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
	if err := jUsers.With(db).InsertMany(ctx,
		&jUser{Name: "Alice", Prefs: jProfile{Theme: "dark", Nickname: "al"}},
		&jUser{Name: "Bob", Prefs: jProfile{Theme: "light"}}, // nickname omitted
		&jUser{Name: "Carol", Prefs: jProfile{Theme: "dark", Nickname: "c"}},
	); err != nil {
		t.Fatalf("InsertMany failed: %v", err)
	}

	names := func(us []*jUser) []string {
		out := make([]string, len(us))
		for i, u := range us {
			out[i] = u.Name
		}
		sort.Strings(out)
		return out
	}
	eq := func(t *testing.T, got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("matched %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("matched %v, want %v", got, want)
			}
		}
	}

	// The ? operator: only the rows whose document has the key. Bob's was
	// omitted, so he is not among them. This is the case pgx's $-numbered
	// parameters could have collided with and do not.
	t.Run("has key", func(t *testing.T) {
		got, err := jUsers.With(db).Where(jUsers.Prefs.HasKey("nickname")).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		eq(t, names(got), []string{"Alice", "Carol"})
	})

	// The @> operator: the rows whose document contains {"theme":"dark"} as a
	// subtree. The value is bound as text and cast to jsonb.
	t.Run("contains", func(t *testing.T) {
		got, err := jUsers.With(db).Where(jUsers.Prefs.Contains(jProfile{Theme: "dark"})).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		eq(t, names(got), []string{"Alice", "Carol"})
	})

	// The ->> extraction, compared as text.
	t.Run("key equals", func(t *testing.T) {
		got, err := jUsers.With(db).Where(jUsers.Prefs.Key("theme").Eq("light")).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		eq(t, names(got), []string{"Bob"})
	})

	t.Run("key not equals", func(t *testing.T) {
		got, err := jUsers.With(db).Where(jUsers.Prefs.Key("theme").NotEq("dark")).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		eq(t, names(got), []string{"Bob"})
	})

	// The operators compose with each other and with typed predicates, their
	// placeholders numbering in together.
	t.Run("composed with a typed predicate", func(t *testing.T) {
		got, err := jUsers.With(db).Where(
			jUsers.Prefs.Key("theme").Eq("dark"),
			jUsers.Prefs.HasKey("nickname"),
			jUsers.Name.NotEq("Carol"),
		).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		eq(t, names(got), []string{"Alice"})
	})
}
