package fixtures

import "github.com/tork-go/orm"

// The models here exercise every feature at once: typed columns of each
// kind, a field promoted from an embedded struct, a native enum, an array,
// a compound unique index, a check constraint, a foreign key with a
// referential action, and a relationship named explicitly.
//
// They exist so one test can prove the parts still compose, which is a
// different question from whether each works alone. The plain Users and
// Posts fixtures stay small on purpose, since most tests want the simplest
// model that makes their point.

// Audit is embedded in Book, so book_created_at resolves through a
// promoted field rather than a direct one.
type Audit struct {
	BookCreatedAt string
}

type Author struct {
	ID   int
	Name string
}

type AuthorModel struct {
	orm.Table[Author]
	ID    *orm.IntColumn
	Name  *orm.StringColumn
	Books orm.HasMany[Book]
}

var Authors = orm.DefineTable[Author]("rich_authors", func(t *orm.TableBuilder[Author]) *AuthorModel {
	return &AuthorModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull().MaxLen(80),
	}
})

type Book struct {
	Audit
	ID       int
	Title    string
	AuthorID int
	Pages    int
	Status   string
	Tags     []string
	Price    *string
}

type BookModel struct {
	orm.Table[Book]
	ID            *orm.IntColumn
	Title         *orm.StringColumn
	AuthorID      *orm.IntColumn
	Pages         *orm.IntColumn
	Status        *orm.EnumColumn
	Tags          *orm.StringArrayColumn
	Price         *orm.NullableStringColumn
	BookCreatedAt *orm.StringColumn
	Author        orm.BelongsTo[Author]
}

// Indexes, Checks and Relations are all optional interfaces on the model,
// and a model is free to implement all three.
func (m *BookModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{orm.NewIndexDef(m.AuthorID, m.Title).Unique()}
}

func (m *BookModel) Checks() []orm.CheckDef {
	return []orm.CheckDef{orm.NewCheckDef("pages > 0")}
}

func (m *BookModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{orm.Via(&m.Author, m.AuthorID)}
}

var Books = orm.DefineTable[Book]("rich_books", func(t *orm.TableBuilder[Book]) *BookModel {
	return &BookModel{
		Table:         t.Table(),
		ID:            t.Int("id").PrimaryKey(),
		Title:         t.String("title").NotNull().MaxLen(200),
		AuthorID:      t.Int("author_id").NotNull().References(Authors.ID).OnDelete(orm.ActionCascade),
		Pages:         t.Int("pages").NotNull(),
		Status:        t.Enum("status", "rich_book_status", "draft", "published"),
		Tags:          t.StringArray("tags"),
		Price:         t.NullableString("price"),
		BookCreatedAt: t.String("book_created_at").NotNull(),
	}
})
