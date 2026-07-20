package schema_test

import (
	"reflect"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/schema"
	"github.com/tork-go/orm/tests/fixtures"
)

func TestExtractSchema_UserTable(t *testing.T) {
	s, err := schema.ExtractSchema(fixtures.User)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	if len(s.Tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(s.Tables))
	}
	users := s.Tables[0]

	if got, want := users.Name, "users"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if len(users.Columns) != 3 {
		t.Fatalf("got %d columns, want 3: %+v", len(users.Columns), users.Columns)
	}

	id, username, email := users.Columns[0], users.Columns[1], users.Columns[2]

	if id.Name != "id" || id.Type.Kind != schema.KindInteger || !id.NotNull {
		t.Errorf("id column = %+v, want Name=id Kind=Integer NotNull=true", id)
	}
	wantUsername := schema.Column{Name: "username", Type: schema.ColumnType{Kind: schema.KindVarchar, Length: 30}, NotNull: true}
	if username != wantUsername {
		t.Errorf("username column = %+v, want %+v", username, wantUsername)
	}
	wantEmail := schema.Column{Name: "email", Type: schema.ColumnType{Kind: schema.KindText}, NotNull: false}
	if email != wantEmail {
		t.Errorf("email column = %+v, want %+v", email, wantEmail)
	}

	if users.PrimaryKey == nil {
		t.Fatal("PrimaryKey is nil, want pk_users on [id]")
	}
	wantPK := schema.PrimaryKey{Name: "pk_users", Columns: []string{"id"}}
	if !reflect.DeepEqual(*users.PrimaryKey, wantPK) {
		t.Errorf("PrimaryKey = %+v, want %+v", *users.PrimaryKey, wantPK)
	}

	if len(users.Uniques) != 1 {
		t.Fatalf("got %d unique constraints, want 1: %+v", len(users.Uniques), users.Uniques)
	}
	wantUnique := schema.UniqueConstraint{Name: "uq_users_username", Columns: []string{"username"}}
	if !reflect.DeepEqual(users.Uniques[0], wantUnique) {
		t.Errorf("Uniques[0] = %+v, want %+v", users.Uniques[0], wantUnique)
	}

	if len(users.ForeignKeys) != 0 {
		t.Errorf("ForeignKeys = %+v, want none", users.ForeignKeys)
	}
}

func TestExtractSchema_PostTable(t *testing.T) {
	s, err := schema.ExtractSchema(fixtures.Post)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	posts := s.Tables[0]

	if got, want := posts.Name, "posts"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if len(posts.Columns) != 4 {
		t.Fatalf("got %d columns, want 4: %+v", len(posts.Columns), posts.Columns)
	}

	content := posts.Columns[2]
	wantContent := schema.Column{Name: "content", Type: schema.ColumnType{Kind: schema.KindText}, NotNull: true}
	if content != wantContent {
		t.Errorf("content column = %+v, want %+v", content, wantContent)
	}

	authorID := posts.Columns[3]
	if authorID.Name != "author_id" || authorID.Type.Kind != schema.KindInteger || !authorID.NotNull {
		t.Errorf("author_id column = %+v, want Name=author_id Kind=Integer NotNull=true", authorID)
	}

	if len(posts.ForeignKeys) != 1 {
		t.Fatalf("got %d foreign keys, want 1: %+v", len(posts.ForeignKeys), posts.ForeignKeys)
	}
	wantFK := schema.ForeignKey{
		Name:              "fk_posts_author_id",
		Columns:           []string{"author_id"},
		ReferencedTable:   "users",
		ReferencedColumns: []string{"id"},
	}
	if !reflect.DeepEqual(posts.ForeignKeys[0], wantFK) {
		t.Errorf("ForeignKeys[0] = %+v, want %+v", posts.ForeignKeys[0], wantFK)
	}
}

func TestExtractSchema_MultipleModels(t *testing.T) {
	s, err := schema.ExtractSchema(fixtures.User, fixtures.Post)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	if len(s.Tables) != 2 {
		t.Fatalf("got %d tables, want 2", len(s.Tables))
	}
	if s.Tables[0].Name != "users" || s.Tables[1].Name != "posts" {
		t.Errorf("table order = [%s, %s], want [users, posts]", s.Tables[0].Name, s.Tables[1].Name)
	}
}

type maxLenOnIntModel struct {
	orm.Table
	N *orm.Column[int]
}

func TestExtractSchema_MaxLenOnNonStringColumn_Error(t *testing.T) {
	m := &maxLenOnIntModel{Table: orm.NewTable("t"), N: orm.NewColumn[int]("n").MaxLen(10)}
	if _, err := schema.ExtractSchema(m); err == nil {
		t.Fatal("expected an error for MaxLen used on a non-string column, got nil")
	}
}

type nonPositiveMaxLenModel struct {
	orm.Table
	S *orm.Column[string]
}

func TestExtractSchema_NonPositiveMaxLenOnStringColumn_Error(t *testing.T) {
	tests := []struct {
		name   string
		maxLen int
	}{
		{name: "zero", maxLen: 0},
		{name: "negative", maxLen: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &nonPositiveMaxLenModel{Table: orm.NewTable("t"), S: orm.NewColumn[string]("s").MaxLen(tt.maxLen)}
			if _, err := schema.ExtractSchema(m); err == nil {
				t.Fatalf("expected an error for MaxLen(%d) on a string column, got nil", tt.maxLen)
			}
		})
	}
}

type unsupportedTypeModel struct {
	orm.Table
	Data *orm.Column[[]byte]
}

func TestExtractSchema_UnsupportedGoType_Error(t *testing.T) {
	m := &unsupportedTypeModel{Table: orm.NewTable("t"), Data: orm.NewColumn[[]byte]("data")}
	if _, err := schema.ExtractSchema(m); err == nil {
		t.Fatal("expected an error for an unsupported Go type, got nil")
	}
}
