package postgres_test

import (
	"testing"

	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/schema"
)

func TestRenderCreateTable_SingleColumnIntegerPK_GetsIdentity(t *testing.T) {
	table := schema.Table{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true},
			{Name: "username", Type: schema.ColumnType{Kind: schema.KindVarchar, Length: 30}, NotNull: true},
			{Name: "email", Type: schema.ColumnType{Kind: schema.KindText}, NotNull: false},
		},
		PrimaryKey: &schema.PrimaryKey{Name: "pk_users", Columns: []string{"id"}},
		Uniques:    []schema.UniqueConstraint{{Name: "uq_users_username", Columns: []string{"username"}}},
	}

	got, err := postgres.Dialect{}.RenderCreateTable(table)
	if err != nil {
		t.Fatalf("RenderCreateTable failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d statements, want 1: %v", len(got), got)
	}
	want := "CREATE TABLE \"users\" (\n" +
		"    \"id\" INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,\n" +
		"    \"username\" VARCHAR(30) NOT NULL,\n" +
		"    \"email\" TEXT,\n" +
		"    CONSTRAINT \"uq_users_username\" UNIQUE (\"username\")\n" +
		")"
	if got[0] != want {
		t.Errorf("RenderCreateTable() =\n%s\nwant\n%s", got[0], want)
	}
}

func TestRenderCreateTable_CompositePrimaryKey_NoIdentity(t *testing.T) {
	table := schema.Table{
		Name: "foo",
		Columns: []schema.Column{
			{Name: "a", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true},
			{Name: "b", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true},
		},
		PrimaryKey: &schema.PrimaryKey{Name: "pk_foo", Columns: []string{"a", "b"}},
	}

	got, err := postgres.Dialect{}.RenderCreateTable(table)
	if err != nil {
		t.Fatalf("RenderCreateTable failed: %v", err)
	}
	want := "CREATE TABLE \"foo\" (\n" +
		"    \"a\" INTEGER NOT NULL,\n" +
		"    \"b\" INTEGER NOT NULL,\n" +
		"    CONSTRAINT \"pk_foo\" PRIMARY KEY (\"a\", \"b\")\n" +
		")"
	if got[0] != want {
		t.Errorf("RenderCreateTable() =\n%s\nwant\n%s", got[0], want)
	}
}

func TestRenderCreateTable_NonIntegerSingleColumnPK_NoIdentity(t *testing.T) {
	table := schema.Table{
		Name: "foo",
		Columns: []schema.Column{
			{Name: "code", Type: schema.ColumnType{Kind: schema.KindVarchar, Length: 10}, NotNull: true},
		},
		PrimaryKey: &schema.PrimaryKey{Name: "pk_foo", Columns: []string{"code"}},
	}

	got, err := postgres.Dialect{}.RenderCreateTable(table)
	if err != nil {
		t.Fatalf("RenderCreateTable failed: %v", err)
	}
	want := "CREATE TABLE \"foo\" (\n" +
		"    \"code\" VARCHAR(10) NOT NULL,\n" +
		"    CONSTRAINT \"pk_foo\" PRIMARY KEY (\"code\")\n" +
		")"
	if got[0] != want {
		t.Errorf("RenderCreateTable() =\n%s\nwant\n%s", got[0], want)
	}
}

func TestRenderCreateTable_AllKinds(t *testing.T) {
	table := schema.Table{
		Name: "kinds",
		Columns: []schema.Column{
			{Name: "a", Type: schema.ColumnType{Kind: schema.KindBoolean}},
			{Name: "b", Type: schema.ColumnType{Kind: schema.KindBigInteger}},
			{Name: "c", Type: schema.ColumnType{Kind: schema.KindFloat}},
			{Name: "d", Type: schema.ColumnType{Kind: schema.KindDouble}},
			{Name: "e", Type: schema.ColumnType{Kind: schema.KindTimestamp}},
		},
	}
	got, err := postgres.Dialect{}.RenderCreateTable(table)
	if err != nil {
		t.Fatalf("RenderCreateTable failed: %v", err)
	}
	want := "CREATE TABLE \"kinds\" (\n" +
		"    \"a\" BOOLEAN,\n" +
		"    \"b\" BIGINT,\n" +
		"    \"c\" REAL,\n" +
		"    \"d\" DOUBLE PRECISION,\n" +
		"    \"e\" TIMESTAMP WITHOUT TIME ZONE\n" +
		")"
	if got[0] != want {
		t.Errorf("RenderCreateTable() =\n%s\nwant\n%s", got[0], want)
	}
}

func TestRenderCreateTable_UnsupportedKind_Error(t *testing.T) {
	table := schema.Table{
		Name:    "foo",
		Columns: []schema.Column{{Name: "x", Type: schema.ColumnType{Kind: schema.Kind(99)}}},
	}
	if _, err := (postgres.Dialect{}).RenderCreateTable(table); err == nil {
		t.Fatal("expected an error for an unsupported column kind, got nil")
	}
}

func TestRenderDropTable(t *testing.T) {
	got := postgres.Dialect{}.RenderDropTable("users")
	want := []string{`DROP TABLE "users"`}
	if !equalSlices(got, want) {
		t.Errorf("RenderDropTable() = %v, want %v", got, want)
	}
}

func TestRenderAddColumn(t *testing.T) {
	tests := []struct {
		name string
		col  schema.Column
		want string
	}{
		{
			name: "not null",
			col:  schema.Column{Name: "age", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true},
			want: `ALTER TABLE "users" ADD COLUMN "age" INTEGER NOT NULL`,
		},
		{
			name: "nullable",
			col:  schema.Column{Name: "age", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: false},
			want: `ALTER TABLE "users" ADD COLUMN "age" INTEGER`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := postgres.Dialect{}.RenderAddColumn("users", tt.col)
			if err != nil {
				t.Fatalf("RenderAddColumn failed: %v", err)
			}
			if !equalSlices(got, []string{tt.want}) {
				t.Errorf("RenderAddColumn() = %v, want [%s]", got, tt.want)
			}
		})
	}
}

func TestRenderAddColumn_UnsupportedKind_Error(t *testing.T) {
	col := schema.Column{Name: "x", Type: schema.ColumnType{Kind: schema.Kind(99)}}
	if _, err := (postgres.Dialect{}).RenderAddColumn("t", col); err == nil {
		t.Fatal("expected an error for an unsupported column kind, got nil")
	}
}

func TestRenderDropColumn(t *testing.T) {
	got := postgres.Dialect{}.RenderDropColumn("users", "age")
	want := []string{`ALTER TABLE "users" DROP COLUMN "age"`}
	if !equalSlices(got, want) {
		t.Errorf("RenderDropColumn() = %v, want %v", got, want)
	}
}

func TestRenderAlterColumnType(t *testing.T) {
	col := schema.Column{Name: "age", Type: schema.ColumnType{Kind: schema.KindBigInteger}}
	got, err := postgres.Dialect{}.RenderAlterColumnType("users", col)
	if err != nil {
		t.Fatalf("RenderAlterColumnType failed: %v", err)
	}
	want := []string{`ALTER TABLE "users" ALTER COLUMN "age" TYPE BIGINT`}
	if !equalSlices(got, want) {
		t.Errorf("RenderAlterColumnType() = %v, want %v", got, want)
	}
}

func TestRenderAlterColumnType_UnsupportedKind_Error(t *testing.T) {
	col := schema.Column{Name: "x", Type: schema.ColumnType{Kind: schema.Kind(99)}}
	if _, err := (postgres.Dialect{}).RenderAlterColumnType("t", col); err == nil {
		t.Fatal("expected an error for an unsupported column kind, got nil")
	}
}

func TestRenderAlterColumnNullability(t *testing.T) {
	tests := []struct {
		notNull bool
		want    string
	}{
		{notNull: true, want: `ALTER TABLE "users" ALTER COLUMN "age" SET NOT NULL`},
		{notNull: false, want: `ALTER TABLE "users" ALTER COLUMN "age" DROP NOT NULL`},
	}
	for _, tt := range tests {
		got := postgres.Dialect{}.RenderAlterColumnNullability("users", "age", tt.notNull)
		if !equalSlices(got, []string{tt.want}) {
			t.Errorf("RenderAlterColumnNullability(notNull=%v) = %v, want [%s]", tt.notNull, got, tt.want)
		}
	}
}

func TestRenderAddPrimaryKey(t *testing.T) {
	pk := schema.PrimaryKey{Name: "pk_users", Columns: []string{"id"}}
	got := postgres.Dialect{}.RenderAddPrimaryKey("users", pk)
	want := []string{`ALTER TABLE "users" ADD CONSTRAINT "pk_users" PRIMARY KEY ("id")`}
	if !equalSlices(got, want) {
		t.Errorf("RenderAddPrimaryKey() = %v, want %v", got, want)
	}
}

func TestRenderDropPrimaryKey(t *testing.T) {
	got := postgres.Dialect{}.RenderDropPrimaryKey("users", "pk_users")
	want := []string{`ALTER TABLE "users" DROP CONSTRAINT "pk_users"`}
	if !equalSlices(got, want) {
		t.Errorf("RenderDropPrimaryKey() = %v, want %v", got, want)
	}
}

func TestRenderAddUnique(t *testing.T) {
	u := schema.UniqueConstraint{Name: "uq_users_username", Columns: []string{"username"}}
	got := postgres.Dialect{}.RenderAddUnique("users", u)
	want := []string{`ALTER TABLE "users" ADD CONSTRAINT "uq_users_username" UNIQUE ("username")`}
	if !equalSlices(got, want) {
		t.Errorf("RenderAddUnique() = %v, want %v", got, want)
	}
}

func TestRenderDropUnique(t *testing.T) {
	got := postgres.Dialect{}.RenderDropUnique("users", "uq_users_username")
	want := []string{`ALTER TABLE "users" DROP CONSTRAINT "uq_users_username"`}
	if !equalSlices(got, want) {
		t.Errorf("RenderDropUnique() = %v, want %v", got, want)
	}
}

func TestRenderAddForeignKey(t *testing.T) {
	fk := schema.ForeignKey{
		Name:              "fk_posts_author_id",
		Columns:           []string{"author_id"},
		ReferencedTable:   "users",
		ReferencedColumns: []string{"id"},
	}
	got := postgres.Dialect{}.RenderAddForeignKey("posts", fk)
	want := []string{`ALTER TABLE "posts" ADD CONSTRAINT "fk_posts_author_id" FOREIGN KEY ("author_id") REFERENCES "users" ("id")`}
	if !equalSlices(got, want) {
		t.Errorf("RenderAddForeignKey() = %v, want %v", got, want)
	}
}

func TestRenderDropForeignKey(t *testing.T) {
	got := postgres.Dialect{}.RenderDropForeignKey("posts", "fk_posts_author_id")
	want := []string{`ALTER TABLE "posts" DROP CONSTRAINT "fk_posts_author_id"`}
	if !equalSlices(got, want) {
		t.Errorf("RenderDropForeignKey() = %v, want %v", got, want)
	}
}

func TestRenderDropTable_QuotesEmbeddedDoubleQuote(t *testing.T) {
	got := postgres.Dialect{}.RenderDropTable(`weird"name`)
	want := []string{`DROP TABLE "weird""name"`}
	if !equalSlices(got, want) {
		t.Errorf("RenderDropTable() = %v, want %v", got, want)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
