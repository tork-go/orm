package schema_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

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

type indexedOnlyModel struct {
	orm.Table
	Email *orm.Column[string]
}

func TestExtractSchema_IndexAlone_ProducesIndexNotUnique(t *testing.T) {
	m := &indexedOnlyModel{Table: orm.NewTable("t"), Email: orm.NewColumn[string]("email").Index()}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	table := s.Tables[0]
	if len(table.Uniques) != 0 {
		t.Errorf("Uniques = %+v, want none", table.Uniques)
	}
	want := []schema.Index{{Name: "ix_t_email", Columns: []string{"email"}}}
	if !reflect.DeepEqual(table.Indexes, want) {
		t.Errorf("Indexes = %+v, want %+v", table.Indexes, want)
	}
}

type indexedAndUniqueModel struct {
	orm.Table
	Email *orm.Column[string]
}

func TestExtractSchema_IndexAndUnique_FoldsIntoUniqueOnly(t *testing.T) {
	m := &indexedAndUniqueModel{Table: orm.NewTable("t"), Email: orm.NewColumn[string]("email").Unique().Index()}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	table := s.Tables[0]
	if len(table.Indexes) != 0 {
		t.Errorf("Indexes = %+v, want none (folded into Uniques)", table.Indexes)
	}
	want := []schema.UniqueConstraint{{Name: "uq_t_email", Columns: []string{"email"}}}
	if !reflect.DeepEqual(table.Uniques, want) {
		t.Errorf("Uniques = %+v, want %+v", table.Uniques, want)
	}
}

type indexerCompoundModel struct {
	orm.Table
	A *orm.Column[int]
	B *orm.Column[int]
}

func (m *indexerCompoundModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{orm.NewIndexDef(m.A, m.B)}
}

func TestExtractSchema_Indexer_UnnamedCompoundIndex_AutoNamed(t *testing.T) {
	m := &indexerCompoundModel{Table: orm.NewTable("t"), A: orm.NewColumn[int]("a"), B: orm.NewColumn[int]("b")}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	want := []schema.Index{{Name: "ix_t_a_b", Columns: []string{"a", "b"}}}
	if !reflect.DeepEqual(s.Tables[0].Indexes, want) {
		t.Errorf("Indexes = %+v, want %+v", s.Tables[0].Indexes, want)
	}
}

type indexerCompoundNamedModel struct {
	orm.Table
	A *orm.Column[int]
	B *orm.Column[int]
}

func (m *indexerCompoundNamedModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{orm.NewIndexDef(m.A, m.B).Named("custom_ix")}
}

func TestExtractSchema_Indexer_NamedCompoundIndex_UsesOverride(t *testing.T) {
	m := &indexerCompoundNamedModel{Table: orm.NewTable("t"), A: orm.NewColumn[int]("a"), B: orm.NewColumn[int]("b")}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	want := []schema.Index{{Name: "custom_ix", Columns: []string{"a", "b"}}}
	if !reflect.DeepEqual(s.Tables[0].Indexes, want) {
		t.Errorf("Indexes = %+v, want %+v", s.Tables[0].Indexes, want)
	}
}

type indexerCompoundUniqueModel struct {
	orm.Table
	A *orm.Column[int]
	B *orm.Column[int]
}

func (m *indexerCompoundUniqueModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{orm.NewIndexDef(m.A, m.B).Unique()}
}

func TestExtractSchema_Indexer_CompoundUnique_LandsInUniques(t *testing.T) {
	m := &indexerCompoundUniqueModel{Table: orm.NewTable("t"), A: orm.NewColumn[int]("a"), B: orm.NewColumn[int]("b")}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	table := s.Tables[0]
	if len(table.Indexes) != 0 {
		t.Errorf("Indexes = %+v, want none", table.Indexes)
	}
	want := []schema.UniqueConstraint{{Name: "uq_t_a_b", Columns: []string{"a", "b"}}}
	if !reflect.DeepEqual(table.Uniques, want) {
		t.Errorf("Uniques = %+v, want %+v", table.Uniques, want)
	}
}

type indexerZeroColumnsModel struct {
	orm.Table
	A *orm.Column[int]
}

func (m *indexerZeroColumnsModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{orm.NewIndexDef()}
}

func TestExtractSchema_IndexerZeroColumns_Error(t *testing.T) {
	m := &indexerZeroColumnsModel{Table: orm.NewTable("t"), A: orm.NewColumn[int]("a")}
	_, err := schema.ExtractSchema(m)
	if err == nil {
		t.Fatal("expected an error for a zero-column index definition, got nil")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "index definition has no columns") {
		t.Errorf("error = %q, want it to contain %q", got, "index definition has no columns")
	}
}

type serverDefaultModel struct {
	orm.Table
	CreatedAt *orm.Column[string]
}

func TestExtractSchema_ServerDefault_Populates(t *testing.T) {
	m := &serverDefaultModel{Table: orm.NewTable("t"), CreatedAt: orm.NewColumn[string]("created_at").ServerDefault("now()")}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	if got := s.Tables[0].Columns[0].ServerDefault; got != "now()" {
		t.Errorf("ServerDefault = %q, want %q", got, "now()")
	}
}

func TestExtractSchema_ServerDefault_Empty_Error(t *testing.T) {
	m := &serverDefaultModel{Table: orm.NewTable("t"), CreatedAt: orm.NewColumn[string]("created_at").ServerDefault("")}
	_, err := schema.ExtractSchema(m)
	if err == nil || !strings.Contains(err.Error(), "ServerDefault must not be empty") {
		t.Fatalf("error = %v, want it to contain %q", err, "ServerDefault must not be empty")
	}
}

type identityConflictModel[T int | int64] struct {
	orm.Table
	ID *orm.Column[T]
}

func TestExtractSchema_ServerDefault_IdentityConflict_Error(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		m := &identityConflictModel[int]{Table: orm.NewTable("t"), ID: orm.NewColumn[int]("id").PrimaryKey().ServerDefault("1")}
		_, err := schema.ExtractSchema(m)
		if err == nil || !strings.Contains(err.Error(), "GENERATED ALWAYS AS IDENTITY") {
			t.Fatalf("error = %v, want it to mention GENERATED ALWAYS AS IDENTITY", err)
		}
	})
	t.Run("int64", func(t *testing.T) {
		m := &identityConflictModel[int64]{Table: orm.NewTable("t"), ID: orm.NewColumn[int64]("id").PrimaryKey().ServerDefault("1")}
		_, err := schema.ExtractSchema(m)
		if err == nil || !strings.Contains(err.Error(), "GENERATED ALWAYS AS IDENTITY") {
			t.Fatalf("error = %v, want it to mention GENERATED ALWAYS AS IDENTITY", err)
		}
	})
}

type nonPKServerDefaultModel struct {
	orm.Table
	ID    *orm.Column[int]
	Count *orm.Column[int]
}

func TestExtractSchema_ServerDefault_NonPKInteger_OK(t *testing.T) {
	m := &nonPKServerDefaultModel{
		Table: orm.NewTable("t"),
		ID:    orm.NewColumn[int]("id").PrimaryKey(),
		Count: orm.NewColumn[int]("count").ServerDefault("0"),
	}
	if _, err := schema.ExtractSchema(m); err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
}

type compositePKServerDefaultModel struct {
	orm.Table
	A *orm.Column[int]
	B *orm.Column[int]
}

func TestExtractSchema_ServerDefault_CompositePK_OK(t *testing.T) {
	m := &compositePKServerDefaultModel{
		Table: orm.NewTable("t"),
		A:     orm.NewColumn[int]("a").PrimaryKey().ServerDefault("0"),
		B:     orm.NewColumn[int]("b").PrimaryKey(),
	}
	if _, err := schema.ExtractSchema(m); err != nil {
		t.Fatalf("composite primary key with a ServerDefault member should be allowed: %v", err)
	}
}

type uuidColumnModel struct {
	orm.Table
	ID *orm.Column[uuid.UUID]
}

func TestExtractSchema_UUIDColumn(t *testing.T) {
	m := &uuidColumnModel{Table: orm.NewTable("t"), ID: orm.NewColumn[uuid.UUID]("id").ServerDefault("gen_random_uuid()")}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	col := s.Tables[0].Columns[0]
	if col.Type.Kind != schema.KindUUID {
		t.Errorf("Type.Kind = %v, want KindUUID", col.Type.Kind)
	}
	if col.ServerDefault != "gen_random_uuid()" {
		t.Errorf("ServerDefault = %q, want %q", col.ServerDefault, "gen_random_uuid()")
	}
}

func TestExtractSchema_MaxLenOnUUIDColumn_Error(t *testing.T) {
	m := &uuidColumnModel{Table: orm.NewTable("t"), ID: orm.NewColumn[uuid.UUID]("id").MaxLen(10)}
	if _, err := schema.ExtractSchema(m); err == nil {
		t.Fatal("expected an error for MaxLen used on a UUID column, got nil")
	}
}
