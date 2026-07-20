package schema_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

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

// --- Numeric ---

type numericModel struct {
	orm.Table
	Amount *orm.Column[decimal.Decimal]
}

func TestExtractSchema_Numeric_Bare(t *testing.T) {
	m := &numericModel{Table: orm.NewTable("t"), Amount: orm.NewColumn[decimal.Decimal]("amount")}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	got := s.Tables[0].Columns[0].Type
	if got.Kind != schema.KindNumeric || got.Precision != 0 || got.Scale != 0 {
		t.Errorf("Type = %+v, want bare KindNumeric with no precision/scale", got)
	}
}

func TestExtractSchema_Numeric_WithPrecisionAndScale(t *testing.T) {
	m := &numericModel{Table: orm.NewTable("t"), Amount: orm.NewColumn[decimal.Decimal]("amount").Numeric(10, 2)}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	want := schema.ColumnType{Kind: schema.KindNumeric, Precision: 10, Scale: 2}
	if got := s.Tables[0].Columns[0].Type; got != want {
		t.Errorf("Type = %+v, want %+v", got, want)
	}
}

func TestExtractSchema_Numeric_OnNonNumericColumn_Error(t *testing.T) {
	m := &maxLenOnIntModel{Table: orm.NewTable("t"), N: orm.NewColumn[int]("n").Numeric(10, 2)}
	if _, err := schema.ExtractSchema(m); err == nil || !strings.Contains(err.Error(), "Numeric is only valid on numeric columns") {
		t.Fatalf("error = %v, want it to contain %q", err, "Numeric is only valid on numeric columns")
	}
}

func TestExtractSchema_Numeric_InvalidPrecisionScale_Error(t *testing.T) {
	tests := []struct {
		name           string
		precision      int
		scale          int
		wantErrContain string
	}{
		{name: "zero precision", precision: 0, scale: 0, wantErrContain: "Numeric precision must be positive"},
		{name: "negative precision", precision: -1, scale: 0, wantErrContain: "Numeric precision must be positive"},
		{name: "negative scale", precision: 5, scale: -1, wantErrContain: "Numeric scale must not be negative"},
		{name: "scale exceeds precision", precision: 5, scale: 6, wantErrContain: "must not exceed precision"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &numericModel{Table: orm.NewTable("t"), Amount: orm.NewColumn[decimal.Decimal]("amount").Numeric(tt.precision, tt.scale)}
			_, err := schema.ExtractSchema(m)
			if err == nil || !strings.Contains(err.Error(), tt.wantErrContain) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErrContain)
			}
		})
	}
}

// --- Enum ---

type enumModel struct {
	orm.Table
	Status *orm.Column[string]
}

func TestExtractSchema_Enum_Populates(t *testing.T) {
	m := &enumModel{Table: orm.NewTable("t"), Status: orm.NewColumn[string]("status").Enum("order_status", "pending", "done")}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	want := schema.ColumnType{Kind: schema.KindEnum, TypeName: "order_status"}
	if got := s.Tables[0].Columns[0].Type; got != want {
		t.Errorf("Type = %+v, want %+v", got, want)
	}
	wantEnum := []schema.EnumType{{Name: "order_status", Values: []string{"pending", "done"}}}
	if !reflect.DeepEqual(s.EnumTypes, wantEnum) {
		t.Errorf("EnumTypes = %+v, want %+v", s.EnumTypes, wantEnum)
	}
}

func TestExtractSchema_Enum_OnNonStringColumn_Error(t *testing.T) {
	m := &maxLenOnIntModel{Table: orm.NewTable("t"), N: orm.NewColumn[int]("n").Enum("t", "a")}
	if _, err := schema.ExtractSchema(m); err == nil || !strings.Contains(err.Error(), "Enum is only valid on a string-kind column") {
		t.Fatalf("error = %v, want it to contain %q", err, "Enum is only valid on a string-kind column")
	}
}

func TestExtractSchema_Enum_EmptyTypeName_Error(t *testing.T) {
	m := &enumModel{Table: orm.NewTable("t"), Status: orm.NewColumn[string]("status").Enum("", "a")}
	if _, err := schema.ExtractSchema(m); err == nil || !strings.Contains(err.Error(), "Enum type name must not be empty") {
		t.Fatalf("error = %v, want it to contain %q", err, "Enum type name must not be empty")
	}
}

func TestExtractSchema_Enum_NoValues_Error(t *testing.T) {
	m := &enumModel{Table: orm.NewTable("t"), Status: orm.NewColumn[string]("status").Enum("order_status")}
	if _, err := schema.ExtractSchema(m); err == nil || !strings.Contains(err.Error(), "Enum must have at least one value") {
		t.Fatalf("error = %v, want it to contain %q", err, "Enum must have at least one value")
	}
}

func TestExtractSchema_Enum_DuplicateValues_Error(t *testing.T) {
	m := &enumModel{Table: orm.NewTable("t"), Status: orm.NewColumn[string]("status").Enum("order_status", "a", "a")}
	if _, err := schema.ExtractSchema(m); err == nil || !strings.Contains(err.Error(), "Enum values must be unique") {
		t.Fatalf("error = %v, want it to contain %q", err, "Enum values must be unique")
	}
}

func TestExtractSchema_Enum_MaxLenConflict_Error(t *testing.T) {
	m := &enumModel{Table: orm.NewTable("t"), Status: orm.NewColumn[string]("status").Enum("order_status", "a").MaxLen(10)}
	if _, err := schema.ExtractSchema(m); err == nil || !strings.Contains(err.Error(), "MaxLen is not valid on an Enum column") {
		t.Fatalf("error = %v, want it to contain %q", err, "MaxLen is not valid on an Enum column")
	}
}

func TestExtractSchema_Enum_NumericConflict_Error(t *testing.T) {
	m := &enumModel{Table: orm.NewTable("t"), Status: orm.NewColumn[string]("status").Enum("order_status", "a").Numeric(5, 0)}
	if _, err := schema.ExtractSchema(m); err == nil || !strings.Contains(err.Error(), "Numeric is not valid on an Enum column") {
		t.Fatalf("error = %v, want it to contain %q", err, "Numeric is not valid on an Enum column")
	}
}

type enumModelB struct {
	orm.Table
	Kind *orm.Column[string]
}

func TestExtractSchema_Enum_CrossModelValueMismatch_Error(t *testing.T) {
	a := &enumModel{Table: orm.NewTable("a"), Status: orm.NewColumn[string]("status").Enum("shared_enum", "x", "y")}
	b := &enumModelB{Table: orm.NewTable("b"), Kind: orm.NewColumn[string]("kind").Enum("shared_enum", "x", "z")}
	_, err := schema.ExtractSchema(a, b)
	if err == nil || !strings.Contains(err.Error(), "declared with different values") {
		t.Fatalf("error = %v, want it to contain %q", err, "declared with different values")
	}
}

func TestExtractSchema_Enum_CrossModelSameValues_Dedupes(t *testing.T) {
	a := &enumModel{Table: orm.NewTable("a"), Status: orm.NewColumn[string]("status").Enum("shared_enum", "x", "y")}
	b := &enumModelB{Table: orm.NewTable("b"), Kind: orm.NewColumn[string]("kind").Enum("shared_enum", "x", "y")}
	s, err := schema.ExtractSchema(a, b)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	if len(s.EnumTypes) != 1 {
		t.Fatalf("EnumTypes = %+v, want exactly one deduped entry", s.EnumTypes)
	}
}

// --- JSON / JSONB ---

type jsonModel struct {
	orm.Table
	Data *orm.Column[string]
}

func TestExtractSchema_JSON(t *testing.T) {
	m := &jsonModel{Table: orm.NewTable("t"), Data: orm.NewColumn[string]("data").JSON()}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	if got := s.Tables[0].Columns[0].Type.Kind; got != schema.KindJSON {
		t.Errorf("Type.Kind = %v, want KindJSON", got)
	}
}

func TestExtractSchema_JSONB(t *testing.T) {
	m := &jsonModel{Table: orm.NewTable("t"), Data: orm.NewColumn[string]("data").JSONB()}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	if got := s.Tables[0].Columns[0].Type.Kind; got != schema.KindJSONB {
		t.Errorf("Type.Kind = %v, want KindJSONB", got)
	}
}

func TestExtractSchema_Serialize_AloneImpliesJSONB(t *testing.T) {
	m := &jsonModel{Table: orm.NewTable("t"), Data: orm.NewColumn[string]("data").Serialize(
		func(s string) ([]byte, error) { return []byte(s), nil },
		func(b []byte) (string, error) { return string(b), nil },
	)}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	if got := s.Tables[0].Columns[0].Type.Kind; got != schema.KindJSONB {
		t.Errorf("Type.Kind = %v, want KindJSONB (Serialize alone implies JSONB)", got)
	}
}

func TestExtractSchema_JSON_MaxLenConflict_Error(t *testing.T) {
	m := &jsonModel{Table: orm.NewTable("t"), Data: orm.NewColumn[string]("data").JSON().MaxLen(10)}
	if _, err := schema.ExtractSchema(m); err == nil || !strings.Contains(err.Error(), "MaxLen is not valid on a JSON/JSONB column") {
		t.Fatalf("error = %v, want it to contain %q", err, "MaxLen is not valid on a JSON/JSONB column")
	}
}

func TestExtractSchema_JSON_NumericConflict_Error(t *testing.T) {
	m := &jsonModel{Table: orm.NewTable("t"), Data: orm.NewColumn[string]("data").JSON().Numeric(5, 0)}
	if _, err := schema.ExtractSchema(m); err == nil || !strings.Contains(err.Error(), "Numeric is not valid on a JSON/JSONB column") {
		t.Fatalf("error = %v, want it to contain %q", err, "Numeric is not valid on a JSON/JSONB column")
	}
}

func TestExtractSchema_JSON_EnumConflict_Error(t *testing.T) {
	m := &jsonModel{Table: orm.NewTable("t"), Data: orm.NewColumn[string]("data").JSON().Enum("t", "a")}
	if _, err := schema.ExtractSchema(m); err == nil || !strings.Contains(err.Error(), "Enum cannot be combined with JSON/JSONB") {
		t.Fatalf("error = %v, want it to contain %q", err, "Enum cannot be combined with JSON/JSONB")
	}
}

// --- Array ---

type stringArrayModel struct {
	orm.Table
	Tags *orm.Column[[]string]
}

func TestExtractSchema_Array_Bare(t *testing.T) {
	m := &stringArrayModel{Table: orm.NewTable("t"), Tags: orm.NewColumn[[]string]("tags")}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	want := schema.ColumnType{Kind: schema.KindArray, Elem: &schema.ColumnType{Kind: schema.KindText}}
	got := s.Tables[0].Columns[0].Type
	if got.Kind != want.Kind || got.Elem == nil || *got.Elem != *want.Elem {
		t.Errorf("Type = %+v, want %+v", got, want)
	}
}

func TestExtractSchema_Array_MaxLenAppliesToElement(t *testing.T) {
	m := &stringArrayModel{Table: orm.NewTable("t"), Tags: orm.NewColumn[[]string]("tags").MaxLen(30)}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	got := s.Tables[0].Columns[0].Type
	want := schema.ColumnType{Kind: schema.KindVarchar, Length: 30}
	if got.Kind != schema.KindArray || got.Elem == nil || *got.Elem != want {
		t.Errorf("Type = %+v, want array of %+v", got, want)
	}
}

type numericArrayModel struct {
	orm.Table
	Amounts *orm.Column[[]decimal.Decimal]
}

func TestExtractSchema_Array_NumericAppliesToElement(t *testing.T) {
	m := &numericArrayModel{Table: orm.NewTable("t"), Amounts: orm.NewColumn[[]decimal.Decimal]("amounts").Numeric(10, 2)}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	got := s.Tables[0].Columns[0].Type
	want := schema.ColumnType{Kind: schema.KindNumeric, Precision: 10, Scale: 2}
	if got.Kind != schema.KindArray || got.Elem == nil || *got.Elem != want {
		t.Errorf("Type = %+v, want array of %+v", got, want)
	}
}

func TestExtractSchema_Array_MaxLenOnNonStringElement_Error(t *testing.T) {
	type intArrayModel struct {
		orm.Table
		Nums *orm.Column[[]int]
	}
	m := &intArrayModel{Table: orm.NewTable("t"), Nums: orm.NewColumn[[]int]("nums").MaxLen(10)}
	_, err := schema.ExtractSchema(m)
	if err == nil || !strings.Contains(err.Error(), "MaxLen is only valid on a string column or a string-array column") {
		t.Fatalf("error = %v, want it to contain %q", err, "MaxLen is only valid on a string column or a string-array column")
	}
}

func TestExtractSchema_Array_NumericOnNonNumericElement_Error(t *testing.T) {
	m := &stringArrayModel{Table: orm.NewTable("t"), Tags: orm.NewColumn[[]string]("tags").Numeric(10, 2)}
	_, err := schema.ExtractSchema(m)
	if err == nil || !strings.Contains(err.Error(), "Numeric is only valid on a numeric column or a numeric-array column") {
		t.Fatalf("error = %v, want it to contain %q", err, "Numeric is only valid on a numeric column or a numeric-array column")
	}
}

func TestExtractSchema_Array_MultiDimensional_Error(t *testing.T) {
	type nestedArrayModel struct {
		orm.Table
		Grid *orm.Column[[][]string]
	}
	m := &nestedArrayModel{Table: orm.NewTable("t"), Grid: orm.NewColumn[[][]string]("grid")}
	_, err := schema.ExtractSchema(m)
	if err == nil || !strings.Contains(err.Error(), "multi-dimensional arrays are not supported") {
		t.Fatalf("error = %v, want it to contain %q", err, "multi-dimensional arrays are not supported")
	}
}

// --- Cross-cutting: array of an enum-tagged type is rejected either way ---

type orderStatus string

type enumArrayModel struct {
	orm.Table
	Statuses *orm.Column[[]orderStatus]
}

func TestExtractSchema_ArrayOfEnumType_WithoutEnumCall_Error(t *testing.T) {
	m := &enumArrayModel{Table: orm.NewTable("t"), Statuses: orm.NewColumn[[]orderStatus]("statuses")}
	if _, err := schema.ExtractSchema(m); err == nil {
		t.Fatal("expected an error for an array of a named string type with no Enum() call, got nil")
	}
}

// --- CHECK constraints ---

type checkExtractModel struct {
	orm.Table
	Age *orm.Column[int]
}

func (m *checkExtractModel) Checks() []orm.CheckDef {
	return []orm.CheckDef{orm.NewCheckDef("age >= 0")}
}

func TestExtractSchema_Checker_UnnamedCheck_AutoNamed(t *testing.T) {
	m := &checkExtractModel{Table: orm.NewTable("accounts"), Age: orm.NewColumn[int]("age")}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	want := []schema.Check{{Name: "ck_accounts_1", Expression: "age >= 0"}}
	if !reflect.DeepEqual(s.Tables[0].Checks, want) {
		t.Errorf("Checks = %+v, want %+v", s.Tables[0].Checks, want)
	}
}

type multiCheckModel struct {
	orm.Table
	Age  *orm.Column[int]
	Cash *orm.Column[int]
}

func (m *multiCheckModel) Checks() []orm.CheckDef {
	return []orm.CheckDef{
		orm.NewCheckDef("age >= 0"),
		orm.NewCheckDef("cash >= 0").Named("ck_accounts_cash_non_negative"),
		orm.NewCheckDef("age < 150"),
	}
}

func TestExtractSchema_Checker_MixedNamedAndUnnamed(t *testing.T) {
	m := &multiCheckModel{Table: orm.NewTable("accounts"), Age: orm.NewColumn[int]("age"), Cash: orm.NewColumn[int]("cash")}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	want := []schema.Check{
		{Name: "ck_accounts_1", Expression: "age >= 0"},
		{Name: "ck_accounts_cash_non_negative", Expression: "cash >= 0"},
		{Name: "ck_accounts_2", Expression: "age < 150"},
	}
	if !reflect.DeepEqual(s.Tables[0].Checks, want) {
		t.Errorf("Checks = %+v, want %+v (unnamed checks numbered among themselves only)", s.Tables[0].Checks, want)
	}
}

type emptyCheckModel struct {
	orm.Table
	Age *orm.Column[int]
}

func (m *emptyCheckModel) Checks() []orm.CheckDef {
	return []orm.CheckDef{orm.NewCheckDef("")}
}

func TestExtractSchema_Checker_EmptyExpression_Error(t *testing.T) {
	m := &emptyCheckModel{Table: orm.NewTable("t"), Age: orm.NewColumn[int]("age")}
	_, err := schema.ExtractSchema(m)
	if err == nil || !strings.Contains(err.Error(), "check definition has no expression") {
		t.Fatalf("error = %v, want it to contain %q", err, "check definition has no expression")
	}
}

// --- Foreign key referential actions ---

type fkActionModel struct {
	orm.Table
	ID       *orm.Column[int]
	AuthorID *orm.ForeignKey[int]
}

func TestExtractSchema_ForeignKey_ActionsDefaultToNoAction(t *testing.T) {
	users := orm.NewColumn[int]("id")
	m := &fkActionModel{
		Table:    orm.NewTable("posts"),
		ID:       orm.NewColumn[int]("id"),
		AuthorID: orm.NewForeignKey("author_id", "users", users),
	}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	fk := s.Tables[0].ForeignKeys[0]
	if fk.OnDelete != schema.ActionNoAction || fk.OnUpdate != schema.ActionNoAction {
		t.Errorf("OnDelete=%v OnUpdate=%v, want both ActionNoAction", fk.OnDelete, fk.OnUpdate)
	}
}

func TestExtractSchema_ForeignKey_ActionsPropagate(t *testing.T) {
	users := orm.NewColumn[int]("id")
	m := &fkActionModel{
		Table:    orm.NewTable("posts"),
		ID:       orm.NewColumn[int]("id"),
		AuthorID: orm.NewForeignKey("author_id", "users", users).OnDelete(orm.ActionCascade).OnUpdate(orm.ActionSetNull),
	}
	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	fk := s.Tables[0].ForeignKeys[0]
	if fk.OnDelete != schema.ActionCascade {
		t.Errorf("OnDelete = %v, want ActionCascade", fk.OnDelete)
	}
	if fk.OnUpdate != schema.ActionSetNull {
		t.Errorf("OnUpdate = %v, want ActionSetNull", fk.OnUpdate)
	}
}

func TestExtractSchema_ForeignKey_EveryAction(t *testing.T) {
	tests := []struct {
		ormAction    orm.ForeignKeyAction
		schemaAction schema.ForeignKeyAction
	}{
		{orm.ActionNoAction, schema.ActionNoAction},
		{orm.ActionCascade, schema.ActionCascade},
		{orm.ActionSetNull, schema.ActionSetNull},
		{orm.ActionSetDefault, schema.ActionSetDefault},
		{orm.ActionRestrict, schema.ActionRestrict},
	}
	for _, tt := range tests {
		users := orm.NewColumn[int]("id")
		m := &fkActionModel{
			Table:    orm.NewTable("posts"),
			ID:       orm.NewColumn[int]("id"),
			AuthorID: orm.NewForeignKey("author_id", "users", users).OnDelete(tt.ormAction),
		}
		s, err := schema.ExtractSchema(m)
		if err != nil {
			t.Fatalf("ExtractSchema failed: %v", err)
		}
		if got := s.Tables[0].ForeignKeys[0].OnDelete; got != tt.schemaAction {
			t.Errorf("orm.%v: OnDelete = %v, want %v", tt.ormAction, got, tt.schemaAction)
		}
	}
}
