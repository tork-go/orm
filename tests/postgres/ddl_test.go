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
			{Name: "f", Type: schema.ColumnType{Kind: schema.KindUUID}},
			{Name: "g", Type: schema.ColumnType{Kind: schema.KindNumeric}},
			{Name: "h", Type: schema.ColumnType{Kind: schema.KindNumeric, Precision: 10, Scale: 2}},
			{Name: "i", Type: schema.ColumnType{Kind: schema.KindEnum, TypeName: "order_status"}},
			{Name: "j", Type: schema.ColumnType{Kind: schema.KindJSON}},
			{Name: "k", Type: schema.ColumnType{Kind: schema.KindJSONB}},
			{Name: "l", Type: schema.ColumnType{Kind: schema.KindArray, Elem: &schema.ColumnType{Kind: schema.KindText}}},
			{Name: "m", Type: schema.ColumnType{Kind: schema.KindArray, Elem: &schema.ColumnType{Kind: schema.KindVarchar, Length: 30}}},
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
		"    \"e\" TIMESTAMP WITHOUT TIME ZONE,\n" +
		"    \"f\" UUID,\n" +
		"    \"g\" NUMERIC,\n" +
		"    \"h\" NUMERIC(10,2),\n" +
		"    \"i\" \"order_status\",\n" +
		"    \"j\" JSON,\n" +
		"    \"k\" JSONB,\n" +
		"    \"l\" TEXT[],\n" +
		"    \"m\" VARCHAR(30)[]\n" +
		")"
	if got[0] != want {
		t.Errorf("RenderCreateTable() =\n%s\nwant\n%s", got[0], want)
	}
}

func TestRenderCreateTable_Checks_Inline(t *testing.T) {
	table := schema.Table{
		Name:    "accounts",
		Columns: []schema.Column{{Name: "age", Type: schema.ColumnType{Kind: schema.KindInteger}}},
		Checks:  []schema.Check{{Name: "ck_accounts_1", Expression: "age >= 0"}},
	}
	got, err := postgres.Dialect{}.RenderCreateTable(table)
	if err != nil {
		t.Fatalf("RenderCreateTable failed: %v", err)
	}
	want := "CREATE TABLE \"accounts\" (\n" +
		"    \"age\" INTEGER,\n" +
		"    CONSTRAINT \"ck_accounts_1\" CHECK (age >= 0)\n" +
		")"
	if got[0] != want {
		t.Errorf("RenderCreateTable() =\n%s\nwant\n%s", got[0], want)
	}
}

func TestRenderCreateTable_ServerDefault(t *testing.T) {
	table := schema.Table{
		Name: "widgets",
		Columns: []schema.Column{
			{Name: "created_at", Type: schema.ColumnType{Kind: schema.KindTimestamp}, NotNull: true, ServerDefault: "now()"},
		},
	}
	got, err := postgres.Dialect{}.RenderCreateTable(table)
	if err != nil {
		t.Fatalf("RenderCreateTable failed: %v", err)
	}
	want := "CREATE TABLE \"widgets\" (\n" +
		"    \"created_at\" TIMESTAMP WITHOUT TIME ZONE DEFAULT now() NOT NULL\n" +
		")"
	if got[0] != want {
		t.Errorf("RenderCreateTable() =\n%s\nwant\n%s (DEFAULT must come before NOT NULL)", got[0], want)
	}
}

// TestRenderCreateTable_IdentityColumnIgnoresServerDefault is a defensive
// case: schema.ExtractSchema already rejects this combination before any
// SQL exists (see schema.validateIdentityServerDefault), but
// RenderCreateTable itself stays safe even against a hand-built
// schema.Table that skipped that validation, by not looking at
// ServerDefault on the identity column at all.
func TestRenderCreateTable_IdentityColumnIgnoresServerDefault(t *testing.T) {
	table := schema.Table{
		Name: "widgets",
		Columns: []schema.Column{
			{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true, ServerDefault: "1"},
		},
		PrimaryKey: &schema.PrimaryKey{Name: "pk_widgets", Columns: []string{"id"}},
	}
	got, err := postgres.Dialect{}.RenderCreateTable(table)
	if err != nil {
		t.Fatalf("RenderCreateTable failed: %v", err)
	}
	want := "CREATE TABLE \"widgets\" (\n" +
		"    \"id\" INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY\n" +
		")"
	if got[0] != want {
		t.Errorf("RenderCreateTable() =\n%s\nwant\n%s (ServerDefault must be ignored on an identity column)", got[0], want)
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

func TestRenderAddColumn_ServerDefault(t *testing.T) {
	col := schema.Column{Name: "active", Type: schema.ColumnType{Kind: schema.KindBoolean}, NotNull: true, ServerDefault: "false"}
	got, err := postgres.Dialect{}.RenderAddColumn("users", col)
	if err != nil {
		t.Fatalf("RenderAddColumn failed: %v", err)
	}
	want := []string{`ALTER TABLE "users" ADD COLUMN "active" BOOLEAN DEFAULT false NOT NULL`}
	if !equalSlices(got, want) {
		t.Errorf("RenderAddColumn() = %v, want %v (DEFAULT must come before NOT NULL)", got, want)
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

func TestRenderAddIndex(t *testing.T) {
	tests := []struct {
		name string
		idx  schema.Index
		want string
	}{
		{
			name: "single column",
			idx:  schema.Index{Name: "ix_posts_author_id", Columns: []string{"author_id"}},
			want: `CREATE INDEX "ix_posts_author_id" ON "posts" ("author_id")`,
		},
		{
			name: "multi column",
			idx:  schema.Index{Name: "ix_posts_author_id_created_at", Columns: []string{"author_id", "created_at"}},
			want: `CREATE INDEX "ix_posts_author_id_created_at" ON "posts" ("author_id", "created_at")`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := postgres.Dialect{}.RenderAddIndex("posts", tt.idx)
			if !equalSlices(got, []string{tt.want}) {
				t.Errorf("RenderAddIndex() = %v, want [%s]", got, tt.want)
			}
		})
	}
}

func TestRenderDropIndex(t *testing.T) {
	got := postgres.Dialect{}.RenderDropIndex("posts", "ix_posts_author_id")
	want := []string{`DROP INDEX "ix_posts_author_id"`}
	if !equalSlices(got, want) {
		t.Errorf("RenderDropIndex() = %v, want %v", got, want)
	}
}

func TestRenderDropIndex_QuotesEmbeddedDoubleQuote(t *testing.T) {
	got := postgres.Dialect{}.RenderDropIndex("posts", `weird"name`)
	want := []string{`DROP INDEX "weird""name"`}
	if !equalSlices(got, want) {
		t.Errorf("RenderDropIndex() = %v, want %v", got, want)
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

func TestRenderAddForeignKey_Actions(t *testing.T) {
	base := schema.ForeignKey{
		Name:              "fk_posts_author_id",
		Columns:           []string{"author_id"},
		ReferencedTable:   "users",
		ReferencedColumns: []string{"id"},
	}
	tests := []struct {
		name   string
		fk     schema.ForeignKey
		suffix string
	}{
		{name: "no action, no clause", fk: base, suffix: ""},
		{name: "on delete cascade", fk: withOnDelete(base, schema.ActionCascade), suffix: " ON DELETE CASCADE"},
		{name: "on delete set null", fk: withOnDelete(base, schema.ActionSetNull), suffix: " ON DELETE SET NULL"},
		{name: "on delete set default", fk: withOnDelete(base, schema.ActionSetDefault), suffix: " ON DELETE SET DEFAULT"},
		{name: "on delete restrict", fk: withOnDelete(base, schema.ActionRestrict), suffix: " ON DELETE RESTRICT"},
		{name: "on update cascade", fk: withOnUpdate(base, schema.ActionCascade), suffix: " ON UPDATE CASCADE"},
		{
			name:   "both on delete and on update",
			fk:     withOnUpdate(withOnDelete(base, schema.ActionCascade), schema.ActionSetNull),
			suffix: " ON DELETE CASCADE ON UPDATE SET NULL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := postgres.Dialect{}.RenderAddForeignKey("posts", tt.fk)
			want := []string{`ALTER TABLE "posts" ADD CONSTRAINT "fk_posts_author_id" FOREIGN KEY ("author_id") REFERENCES "users" ("id")` + tt.suffix}
			if !equalSlices(got, want) {
				t.Errorf("RenderAddForeignKey() = %v, want %v", got, want)
			}
		})
	}
}

func withOnDelete(fk schema.ForeignKey, a schema.ForeignKeyAction) schema.ForeignKey {
	fk.OnDelete = a
	return fk
}

func withOnUpdate(fk schema.ForeignKey, a schema.ForeignKeyAction) schema.ForeignKey {
	fk.OnUpdate = a
	return fk
}

func TestRenderDropForeignKey(t *testing.T) {
	got := postgres.Dialect{}.RenderDropForeignKey("posts", "fk_posts_author_id")
	want := []string{`ALTER TABLE "posts" DROP CONSTRAINT "fk_posts_author_id"`}
	if !equalSlices(got, want) {
		t.Errorf("RenderDropForeignKey() = %v, want %v", got, want)
	}
}

func TestRenderAddCheck(t *testing.T) {
	c := schema.Check{Name: "ck_accounts_1", Expression: "age >= 0"}
	got := postgres.Dialect{}.RenderAddCheck("accounts", c)
	want := []string{`ALTER TABLE "accounts" ADD CONSTRAINT "ck_accounts_1" CHECK (age >= 0)`}
	if !equalSlices(got, want) {
		t.Errorf("RenderAddCheck() = %v, want %v", got, want)
	}
}

func TestRenderDropCheck(t *testing.T) {
	got := postgres.Dialect{}.RenderDropCheck("accounts", "ck_accounts_1")
	want := []string{`ALTER TABLE "accounts" DROP CONSTRAINT "ck_accounts_1"`}
	if !equalSlices(got, want) {
		t.Errorf("RenderDropCheck() = %v, want %v", got, want)
	}
}

func TestRenderCreateEnumType(t *testing.T) {
	e := schema.EnumType{Name: "order_status", Values: []string{"pending", "done"}}
	got := postgres.Dialect{}.RenderCreateEnumType(e)
	want := []string{`CREATE TYPE "order_status" AS ENUM ('pending', 'done')`}
	if !equalSlices(got, want) {
		t.Errorf("RenderCreateEnumType() = %v, want %v", got, want)
	}
}

func TestRenderCreateEnumType_QuotesEmbeddedSingleQuote(t *testing.T) {
	e := schema.EnumType{Name: "weird", Values: []string{"it's odd"}}
	got := postgres.Dialect{}.RenderCreateEnumType(e)
	want := []string{`CREATE TYPE "weird" AS ENUM ('it''s odd')`}
	if !equalSlices(got, want) {
		t.Errorf("RenderCreateEnumType() = %v, want %v", got, want)
	}
}

func TestRenderDropEnumType(t *testing.T) {
	got := postgres.Dialect{}.RenderDropEnumType("order_status")
	want := []string{`DROP TYPE "order_status"`}
	if !equalSlices(got, want) {
		t.Errorf("RenderDropEnumType() = %v, want %v", got, want)
	}
}

func TestRenderAddEnumValue(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		before string
		after  string
		want   string
	}{
		{name: "append", value: "cancelled", want: `ALTER TYPE "order_status" ADD VALUE 'cancelled'`},
		{name: "before", value: "new", before: "pending", want: `ALTER TYPE "order_status" ADD VALUE 'new' BEFORE 'pending'`},
		{name: "after", value: "shipped", after: "paid", want: `ALTER TYPE "order_status" ADD VALUE 'shipped' AFTER 'paid'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := postgres.Dialect{}.RenderAddEnumValue("order_status", tt.value, tt.before, tt.after)
			if !equalSlices(got, []string{tt.want}) {
				t.Errorf("RenderAddEnumValue() = %v, want [%s]", got, tt.want)
			}
		})
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
