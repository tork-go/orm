package orm_test

import (
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/tests/fixtures"
)

func TestColumns_UserModel(t *testing.T) {
	cols := orm.Columns(fixtures.Users)

	names := columnNames(cols)
	want := []string{"id", "username", "email"}
	if !equalStrings(names, want) {
		t.Errorf("Columns(User) names = %v, want %v", names, want)
	}
}

func TestColumns_PostModel_IncludesForeignKey(t *testing.T) {
	cols := orm.Columns(fixtures.Posts)

	names := columnNames(cols)
	want := []string{"id", "title", "content", "author_id"}
	if !equalStrings(names, want) {
		t.Errorf("Columns(Post) names = %v, want %v (foreign key column must be included)", names, want)
	}
}

func TestForeignKeys_PostModel(t *testing.T) {
	fks := orm.ForeignKeys(fixtures.Posts)

	if len(fks) != 1 {
		t.Fatalf("ForeignKeys(Post) returned %d entries, want 1", len(fks))
	}
	fk := fks[0]
	if got, want := fk.Name(), "author_id"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if got, want := fk.ReferencedTable(), "users"; got != want {
		t.Errorf("ReferencedTable() = %q, want %q", got, want)
	}
	if got, want := fk.ReferencedColumn(), "id"; got != want {
		t.Errorf("ReferencedColumn() = %q, want %q", got, want)
	}
}

func TestForeignKeys_UserModel_None(t *testing.T) {
	if fks := orm.ForeignKeys(fixtures.Users); len(fks) != 0 {
		t.Errorf("ForeignKeys(User) = %v, want none", fks)
	}
}

// strandedEntity is declared in relationship_test.go and reused here as a
// stand-in row type; what matters is that markers are not columns.

func TestColumns_ExcludesTableAndRelationshipFields(t *testing.T) {
	type Model struct {
		orm.Table[orm.NoEntity]
		ID       *orm.Column[int]
		Children orm.HasMany[strandedEntity]
		Parent   orm.BelongsTo[strandedEntity]
	}
	m := &Model{Table: orm.NewTable[orm.NoEntity]("models"), ID: orm.NewColumn[int]("id")}

	cols := orm.Columns(m)
	if len(cols) != 1 || cols[0].Name() != "id" {
		t.Errorf("Columns(m) = %v, want only the id column", columnNames(cols))
	}
}

func TestColumns_SkipsNilColumnField(t *testing.T) {
	type Model struct {
		orm.Table[orm.NoEntity]
		ID    *orm.Column[int]
		Extra *orm.Column[string] // deliberately left nil
	}
	m := &Model{Table: orm.NewTable[orm.NoEntity]("models"), ID: orm.NewColumn[int]("id")}

	cols := orm.Columns(m)
	if len(cols) != 1 || cols[0].Name() != "id" {
		t.Errorf("Columns(m) = %v, want only the id column (nil field must be skipped, not panic)", columnNames(cols))
	}
}

func columnNames(cols []orm.ColumnMeta) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name()
	}
	return names
}

func equalStrings(a, b []string) bool {
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

func BenchmarkColumns(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = orm.Columns(fixtures.Posts)
	}
}

func BenchmarkForeignKeys(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = orm.ForeignKeys(fixtures.Posts)
	}
}
