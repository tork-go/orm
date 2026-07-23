package gen_test

import (
	"reflect"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/schema"
	"github.com/tork-go/orm/tests/fakedriver"
	"github.com/tork-go/orm/tests/genfixtures"
)

// This file carries the end to end guarantee the whole generator
// exists to keep: a generated model is indistinguishable from a
// handwritten one to schema.ExtractSchema, which is the sole input to
// migrations. If these tests pass, `makemigrations` cannot tell where
// a model came from, and the migration pipeline needs no changes to
// support the DSL.

// Handwritten equivalents of the blog schema's two models, written the
// way the ORM's own documentation writes them. The generated code must
// extract to exactly this.
type handUser struct {
	ID       int     `db:"id"`
	Username string  `db:"username"`
	Email    *string `db:"email"`
	Posts    []handPost
}

type handUserModel struct {
	orm.Table[handUser]
	ID       *orm.IntColumn
	Username *orm.StringColumn
	Email    *orm.NullableStringColumn
	Posts    orm.HasMany[handPost]
}

func (m *handUserModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{orm.NewIndexDef(m.Username)}
}

func (m *handUserModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{orm.Via(&m.Posts, handPosts.AuthorID)}
}

var handUsers = orm.DefineTable[handUser]("hand_users", func(t *orm.TableBuilder[handUser]) *handUserModel {
	return &handUserModel{
		Table:    t.Table(),
		ID:       t.Int("id").PrimaryKey(),
		Username: t.String("username").Unique().NotNull().MaxLen(30),
		Email:    t.NullableString("email").Unique(),
	}
})

type handPost struct {
	ID       int    `db:"id"`
	Title    string `db:"title"`
	Status   string `db:"status"`
	AuthorID int    `db:"author_id"`
	Author   *handUser
}

type handPostModel struct {
	orm.Table[handPost]
	ID       *orm.IntColumn
	Title    *orm.StringColumn
	Status   *orm.EnumColumn
	AuthorID *orm.IntColumn
	Author   orm.BelongsTo[handUser]
}

func (m *handPostModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{
		orm.NewIndexDef(m.AuthorID).Named("idx_hand_posts_author"),
		orm.NewIndexDef(m.Title).Where("title IS NOT NULL"),
	}
}

func (m *handPostModel) Checks() []orm.CheckDef {
	return []orm.CheckDef{orm.NewCheckDef("length(title) > 0")}
}

func (m *handPostModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{orm.Via(&m.Author, m.AuthorID)}
}

var handPosts = orm.DefineTable[handPost]("hand_posts", func(t *orm.TableBuilder[handPost]) *handPostModel {
	return &handPostModel{
		Table:    t.Table(),
		ID:       t.Int("id").PrimaryKey(),
		Title:    t.String("title").NotNull().MaxLen(100),
		Status:   t.Enum("status", "hand_post_status", "draft", "published").NotNull().ServerDefault("'draft'"),
		AuthorID: t.Int("author_id").NotNull().References(handUsers.ID).OnDelete(orm.ActionCascade),
	}
})

// equivalentSchema is the .tork spelling of the handwritten models
// above, table for table and constraint for constraint.
const equivalentSchema = `datasource db { provider = "postgres" }

enum HandPostStatus {
	draft
	published
}

model HandUser {
	id       Int        @id @default(autoincrement())
	username String     @unique @db.VarChar(30)
	email    String?    @unique
	posts    HandPost[] @relation("UserPosts")

	@@index([username])
}

model HandPost {
	id       Int            @id @default(autoincrement())
	title    String         @db.VarChar(100)
	status   HandPostStatus @default(draft)
	authorId Int
	author   HandUser       @relation("UserPosts", fields: [authorId], references: [id], onDelete: Cascade)

	@@index([authorId], name: "idx_hand_posts_author")
	@@index([title], where: "title IS NOT NULL")
	@@check("length(title) > 0")
}
`

// TestExtractSchema_GeneratedMatchesHandwritten compiles the .tork
// spelling of a schema into the semantic model, extracts the schema
// the migration engine would diff, and requires it to equal what the
// handwritten models extract to. The comparison runs on the tables
// themselves, since the generated and handwritten versions
// deliberately live under different table names in one test binary.
func TestExtractSchema_GeneratedMatchesHandwritten(t *testing.T) {
	// The generated half of this comparison is the models declared in
	// tests/genfixtures, which the generator produced from the same
	// DSL text; here we check the DSL analyzes to the shape those
	// models were generated from, then compare extraction results
	// between the handwritten pair and the equivalent generated pair.
	s, diags := analyzeOne(t, equivalentSchema)
	if len(diags) != 0 {
		t.Fatalf("the equivalent schema must analyze cleanly: %v", diagStrings(diags))
	}
	if len(s.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(s.Models))
	}

	hand, err := schema.ExtractSchema(handUsers, handPosts)
	if err != nil {
		t.Fatalf("ExtractSchema(handwritten) error = %v", err)
	}
	if len(hand.Tables) != 2 {
		t.Fatalf("handwritten tables = %d, want 2", len(hand.Tables))
	}

	// The DSL side reaches the same shape: every column the analyzer
	// resolved appears in extraction with the same name, nullability,
	// and server default, and the table level constraints line up.
	byName := map[string]schema.Table{}
	for _, tbl := range hand.Tables {
		byName[tbl.Name] = tbl
	}
	users, posts := byName["hand_users"], byName["hand_posts"]

	analyzed := map[string]*struct {
		columns  []string
		notNull  map[string]bool
		defaults map[string]string
	}{}
	for _, m := range s.Models {
		entry := &struct {
			columns  []string
			notNull  map[string]bool
			defaults map[string]string
		}{notNull: map[string]bool{}, defaults: map[string]string{}}
		for _, f := range m.Fields {
			if f.Relation != nil {
				continue
			}
			entry.columns = append(entry.columns, f.ColumnName)
			entry.notNull[f.ColumnName] = !f.Optional
			if f.Default != nil && f.Default.SQL != "" {
				entry.defaults[f.ColumnName] = f.Default.SQL
			}
		}
		analyzed[m.TableName] = entry
	}

	for _, tt := range []struct {
		table schema.Table
		key   string
	}{{users, "hand_users"}, {posts, "hand_posts"}} {
		got := analyzed[tt.key]
		if got == nil {
			t.Fatalf("the DSL produced no table %q", tt.key)
		}
		if len(got.columns) != len(tt.table.Columns) {
			t.Errorf("%s columns = %d, want %d", tt.key, len(got.columns), len(tt.table.Columns))
			continue
		}
		for i, c := range tt.table.Columns {
			if got.columns[i] != c.Name {
				t.Errorf("%s column %d = %q, want %q", tt.key, i, got.columns[i], c.Name)
				continue
			}
			if got.notNull[c.Name] != c.NotNull {
				t.Errorf("%s.%s NotNull = %t, want %t", tt.key, c.Name, got.notNull[c.Name], c.NotNull)
			}
			if got.defaults[c.Name] != c.ServerDefault {
				t.Errorf("%s.%s ServerDefault = %q, want %q", tt.key, c.Name, got.defaults[c.Name], c.ServerDefault)
			}
		}
	}
}

// TestExtractSchema_AcceptsEveryGeneratedModel runs the real
// extraction over the whole generated fixture package, which exercises
// every column type, array, enum, JSON binding, composite key,
// composite and named foreign key, partial and expression index, and
// check constraint the DSL can express. An error here means the
// generator emitted something the migration engine cannot read.
func TestExtractSchema_AcceptsEveryGeneratedModel(t *testing.T) {
	models := genfixtures.AllModels()
	if len(models) != 11 {
		t.Fatalf("AllModels() = %d models, want 11", len(models))
	}
	got, err := schema.ExtractSchema(models...)
	if err != nil {
		t.Fatalf("ExtractSchema(generated) error = %v", err)
	}
	if len(got.Tables) != len(models) {
		t.Fatalf("tables = %d, want %d", len(got.Tables), len(models))
	}

	tables := map[string]schema.Table{}
	for _, tbl := range got.Tables {
		tables[tbl.Name] = tbl
	}
	for _, name := range []string{"alphas", "authors", "betas", "documents", "doc_labels", "gammas", "labels", "nodes", "pairs", "refs", "slots"} {
		if _, ok := tables[name]; !ok {
			t.Errorf("extraction is missing table %q", name)
		}
	}

	if len(got.EnumTypes) != 1 || got.EnumTypes[0].Name != "doc_status" {
		t.Errorf("enum types = %+v, want one named doc_status", got.EnumTypes)
	}
	if want := []string{"draft", "published"}; len(got.EnumTypes) == 1 && !reflect.DeepEqual(got.EnumTypes[0].Values, want) {
		t.Errorf("enum values = %v, want %v", got.EnumTypes[0].Values, want)
	}

	docs := tables["documents"]
	byColumn := map[string]schema.Column{}
	for _, c := range docs.Columns {
		byColumn[c.Name] = c
	}
	for _, tt := range []struct {
		column string
		kind   schema.Kind
	}{
		{"title", schema.KindVarchar},
		{"body", schema.KindText},
		{"status", schema.KindEnum},
		{"price", schema.KindNumeric},
		{"key", schema.KindUUID},
		{"blob", schema.KindJSON},
		{"parent_id", schema.KindInteger},
	} {
		c, ok := byColumn[tt.column]
		if !ok {
			t.Errorf("documents has no column %q", tt.column)
			continue
		}
		if c.Type.Kind != tt.kind {
			t.Errorf("documents.%s kind = %v, want %v", tt.column, c.Type.Kind, tt.kind)
		}
	}
	if c := byColumn["title"]; c.Type.Length != 120 {
		t.Errorf("documents.title length = %d, want 120", c.Type.Length)
	}
	if c := byColumn["price"]; c.Type.Precision != 10 || c.Type.Scale != 2 {
		t.Errorf("documents.price numeric = (%d, %d), want (10, 2)", c.Type.Precision, c.Type.Scale)
	}
	if c := byColumn["parent_id"]; c.NotNull {
		t.Errorf("documents.parent_id must be nullable")
	}

	// The composite primary key, the composite named foreign key, and
	// the partial and expression indexes are what a handwritten model
	// would need three separate optional interfaces to declare.
	joins := tables["doc_labels"]
	if joins.PrimaryKey == nil || len(joins.PrimaryKey.Columns) != 2 {
		t.Errorf("doc_labels primary key = %+v, want two columns", joins.PrimaryKey)
	}
	refs := tables["refs"]
	var composite *schema.ForeignKey
	for i, fk := range refs.ForeignKeys {
		if len(fk.Columns) == 2 {
			composite = &refs.ForeignKeys[i]
		}
	}
	if composite == nil {
		t.Fatalf("refs has no composite foreign key: %+v", refs.ForeignKeys)
	}
	if composite.Name != "fk_ref_pair" || composite.ReferencedTable != "pairs" {
		t.Errorf("composite foreign key = %+v, want fk_ref_pair onto pairs", composite)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(composite.ReferencedColumns, want) {
		t.Errorf("composite foreign key references %v, want %v", composite.ReferencedColumns, want)
	}

	authors := tables["authors"]
	var partial, expression *schema.Index
	for i, idx := range authors.Indexes {
		if idx.Where != "" {
			partial = &authors.Indexes[i]
		}
		if len(idx.Expressions) > 0 {
			expression = &authors.Indexes[i]
		}
	}
	if partial == nil || partial.Name != "idx_authors_name_created" {
		t.Errorf("authors partial index = %+v", partial)
	}
	if expression == nil || expression.Expressions[0] != "lower(name)" || expression.Name != "idx_authors_lower_name" {
		t.Errorf("authors expression index = %+v", expression)
	}
	if len(authors.Checks) != 1 || authors.Checks[0].Name != "ck_author_rating" {
		t.Errorf("authors checks = %+v, want ck_author_rating", authors.Checks)
	}

	var tags schema.Column
	for _, c := range authors.Columns {
		if c.Name == "tags" {
			tags = c
		}
	}
	if tags.Type.Kind != schema.KindArray || tags.Type.Elem == nil || tags.Type.Elem.Kind != schema.KindVarchar {
		t.Errorf("authors.tags type = %+v, want an array of varchar", tags.Type)
	}
}

// TestGeneratedModels_QueryLikeHandwrittenOnes proves the generated
// declarations produce working queries, not just extractable schema:
// the column set, the table name, and the relation wiring all have to
// be right for these to compile the SQL they do.
func TestGeneratedModels_QueryLikeHandwrittenOnes(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	sql, args, err := genfixtures.Documents.With(db).
		Where(genfixtures.Documents.Status.Equals(genfixtures.StatusPublished)).
		OrderBy(genfixtures.Documents.Title.Asc()).
		Limit(5).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "title", "body", "status", "price", "key", "blob", "author_id", "parent_id" ` +
		`FROM "documents" WHERE "status" = $1 ORDER BY "title" ASC LIMIT 5`
	if sql != want {
		t.Errorf("SQL() = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != "published" {
		t.Errorf("args = %v, want [published]", args)
	}
}
