//go:build integration

package postgres_test

import (
	"context"
	"sort"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
)

// Contains, StartsWith and EndsWith escape the caller's substring, and the
// dialect emits an ESCAPE clause naming the character they escaped with.
// Those two halves are written in different packages and only agree if
// both are right, so this runs the pair against a real database.
//
// The failure this guards is silent: an escape that does not take turns
// Contains("50%") into a prefix match against everything starting "50",
// which returns plausible wrong rows rather than an error.
func TestLikeEscaping_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	d := postgres.Dialect{}
	conn, err := d.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), `DROP TABLE IF EXISTS test_like_escaping`)
	})

	if _, err := conn.Exec(ctx, `DROP TABLE IF EXISTS test_like_escaping`); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE test_like_escaping (name TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO test_like_escaping (name) VALUES
		('50% off'), ('5000 units'), ('a_b'), ('axb'), ('back\slash'), ('backXslash')`); err != nil {
		t.Fatalf("INSERT failed: %v", err)
	}

	name := orm.NewStringColumn("name")

	tests := []struct {
		label string
		pred  orm.Predicate
		want  []string
	}{
		// Without escaping, % would match anything and this would also
		// return "5000 units".
		{"percent is literal", name.Contains("50%"), []string{"50% off"}},
		// Without escaping, _ would match any single character and this
		// would also return "axb".
		{"underscore is literal", name.Contains("a_b"), []string{"a_b"}},
		// The escape character itself has to survive being escaped.
		{"backslash is literal", name.Contains(`back\slash`), []string{`back\slash`}},
		{"prefix", name.StartsWith("50%"), []string{"50% off"}},
		{"suffix", name.EndsWith("units"), []string{"5000 units"}},
		// A pattern written by the caller keeps its wildcards.
		{"explicit pattern keeps wildcards", name.Like("50%"), []string{"50% off", "5000 units"}},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			p, ok := tt.pred.(orm.Pattern)
			if !ok {
				t.Fatalf("predicate is %T, want orm.Pattern", tt.pred)
			}
			where := d.RenderLike(d.QuoteIdent("name"), d.Placeholder(1), p.CaseInsensitive)
			sql := `SELECT name FROM test_like_escaping WHERE ` + where + ` ORDER BY name`

			rows, err := conn.Query(ctx, sql, p.Value)
			if err != nil {
				t.Fatalf("Query(%s) with %q failed: %v", sql, p.Value, err)
			}
			defer rows.Close()

			var got []string
			for rows.Next() {
				var s string
				if err := rows.Scan(&s); err != nil {
					t.Fatalf("Scan failed: %v", err)
				}
				got = append(got, s)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("Err() = %v", err)
			}

			sort.Strings(got)
			want := append([]string(nil), tt.want...)
			sort.Strings(want)
			if len(got) != len(want) {
				t.Fatalf("pattern %q matched %v, want %v", p.Value, got, want)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Errorf("pattern %q matched %v, want %v", p.Value, got, want)
					break
				}
			}
		})
	}
}

// ILIKE has to fold case while still honouring the escape.
func TestILikeEscaping_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	d := postgres.Dialect{}
	conn, err := d.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), `DROP TABLE IF EXISTS test_ilike_escaping`)
	})

	if _, err := conn.Exec(ctx, `DROP TABLE IF EXISTS test_ilike_escaping`); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE test_ilike_escaping (name TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO test_ilike_escaping (name) VALUES ('ALICE'), ('bob')`); err != nil {
		t.Fatalf("INSERT failed: %v", err)
	}

	p := orm.NewStringColumn("name").ILike("alice").(orm.Pattern)
	where := d.RenderLike(d.QuoteIdent("name"), d.Placeholder(1), p.CaseInsensitive)

	var got string
	row := conn.QueryRow(ctx, `SELECT name FROM test_ilike_escaping WHERE `+where, p.Value)
	if err := row.Scan(&got); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if got != "ALICE" {
		t.Errorf("ILIKE %q matched %q, want ALICE", p.Value, got)
	}
}
