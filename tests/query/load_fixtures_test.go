package query_test

import (
	"context"
	"errors"
	"strings"

	"github.com/tork-go/orm"
)

// A schema with one of every relationship shape, and row types that carry the
// fields the loaded rows go into.
//
// Those fields match no column, which is what lets them exist: the entity
// mapping ignores a field no column is named after, so a row type can hold
// related rows alongside its own.

type Author struct {
	ID   int
	Name string

	Books []Book // HasMany
	Desk  *Desk  // HasOne
}

type AuthorModel struct {
	orm.Table[Author]
	ID    *orm.IntColumn
	Name  *orm.StringColumn
	Books orm.HasMany[Book]
	Desk  orm.HasOne[Desk]
}

var Authors = orm.DefineTable[Author]("authors", func(t *orm.TableBuilder[Author]) *AuthorModel {
	return &AuthorModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
	}
})

type Book struct {
	ID       int
	AuthorID int
	Title    string

	Author  *Author  // BelongsTo
	Tags    []Tag    // ManyToMany
	Reviews []Review // HasMany, over a key that may be NULL
}

type BookModel struct {
	orm.Table[Book]
	ID       *orm.IntColumn
	AuthorID *orm.IntColumn
	Title    *orm.StringColumn
	Author   orm.BelongsTo[Author]
	Tags     orm.ManyToMany[Tag]
	Reviews  orm.HasMany[Review]
}

// The join table is named here rather than inferred: nothing about a
// ManyToMany[Tag] says which table joins the two.
func (m *BookModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{orm.Through(&m.Tags, BookTags.BookID, BookTags.TagID)}
}

var Books = orm.DefineTable[Book]("books", func(t *orm.TableBuilder[Book]) *BookModel {
	return &BookModel{
		Table:    t.Table(),
		ID:       t.Int("id").PrimaryKey(),
		AuthorID: t.Int("author_id").NotNull().References(Authors.ID),
		Title:    t.String("title").NotNull(),
	}
})

// Desk is the far side of a HasOne: the key lives here, made unique, which is
// what makes it one rather than many.
type Desk struct {
	ID       int
	AuthorID int
	Colour   string
}

type DeskModel struct {
	orm.Table[Desk]
	ID       *orm.IntColumn
	AuthorID *orm.IntColumn
	Colour   *orm.StringColumn
}

var Desks = orm.DefineTable[Desk]("desks", func(t *orm.TableBuilder[Desk]) *DeskModel {
	return &DeskModel{
		Table:    t.Table(),
		ID:       t.Int("id").PrimaryKey(),
		AuthorID: t.Int("author_id").Unique().NotNull().References(Authors.ID),
		Colour:   t.String("colour").NotNull(),
	}
})

// Review's key into books is nullable, which is what makes it the fixture for
// a key that matches nothing on either side.
type Review struct {
	ID     int
	BookID *int
	Text   string

	Book *Book
}

type ReviewModel struct {
	orm.Table[Review]
	ID     *orm.IntColumn
	BookID *orm.NullableIntColumn
	Text   *orm.StringColumn
	Book   orm.BelongsTo[Book]
}

var Reviews = orm.DefineTable[Review]("reviews", func(t *orm.TableBuilder[Review]) *ReviewModel {
	return &ReviewModel{
		Table:  t.Table(),
		ID:     t.Int("id").PrimaryKey(),
		BookID: t.NullableInt("book_id").References(Books.ID),
		Text:   t.String("text").NotNull(),
	}
})

type Tag struct {
	ID   int
	Name string
}

type TagModel struct {
	orm.Table[Tag]
	ID   *orm.IntColumn
	Name *orm.StringColumn
}

var Tags = orm.DefineTable[Tag]("tags", func(t *orm.TableBuilder[Tag]) *TagModel {
	return &TagModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
	}
})

// The join table is an ordinary model with two keys and no markers of its own.
type BookTag struct {
	BookID int
	TagID  int
}

type BookTagModel struct {
	orm.Table[BookTag]
	BookID *orm.IntColumn
	TagID  *orm.IntColumn
}

var BookTags = orm.DefineTable[BookTag]("book_tags", func(t *orm.TableBuilder[BookTag]) *BookTagModel {
	return &BookTagModel{
		Table:  t.Table(),
		BookID: t.Int("book_id").PrimaryKey().References(Books.ID),
		TagID:  t.Int("tag_id").PrimaryKey().References(Tags.ID),
	}
})

// errBookRefused is what the hook returns for the one title that asks it to,
// so a refusal on a loaded row is testable.
var errBookRefused = errors.New("this book refuses to load")

// A hook on the related row type, so a test can tell that loaded rows go
// through the same path a queried row does.
func (b *Book) AfterLoad(context.Context) error {
	if b.Title == "!refuse" {
		return errBookRefused
	}
	b.Title = strings.ToLower(strings.TrimSpace(b.Title))
	return nil
}

// A model whose relationship cannot be resolved: nothing on books references
// this table, so there is no key to join on.
type Unjoined struct {
	ID    int
	Name  string
	Books []Book
}

type UnjoinedModel struct {
	orm.Table[Unjoined]
	ID    *orm.IntColumn
	Name  *orm.StringColumn
	Books orm.HasMany[Book]
}

var Unjoinable = orm.DefineTable[Unjoined]("unjoinable", func(t *orm.TableBuilder[Unjoined]) *UnjoinedModel {
	return &UnjoinedModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
	}
})

// The two below have relationships that resolve perfectly well, so what fails
// is the row type rather than the schema. A leaf carries a key to each, which
// is what makes their relationships resolvable in the first place.

// A row type with no field for the relationship its model declares, which is
// legal until something asks for the rows.
type Orphan struct {
	ID int
}

type OrphanModel struct {
	orm.Table[Orphan]
	ID     *orm.IntColumn
	Leaves orm.HasMany[Leaf]
}

var Orphans = orm.DefineTable[Orphan]("orphans", func(t *orm.TableBuilder[Orphan]) *OrphanModel {
	return &OrphanModel{Table: t.Table(), ID: t.Int("id").PrimaryKey()}
})

// A row type whose fields are the wrong shape: a HasMany loads many rows and
// a single value cannot hold them, while a HasOne loads one and a slice is
// not what it means.
type Mistyped struct {
	ID     int
	Leaves Leaf
	Solo   []Leaf
}

type MistypedModel struct {
	orm.Table[Mistyped]
	ID     *orm.IntColumn
	Leaves orm.HasMany[Leaf]
	Solo   orm.HasOne[Leaf]
}

var Mistypeds = orm.DefineTable[Mistyped]("mistyped", func(t *orm.TableBuilder[Mistyped]) *MistypedModel {
	return &MistypedModel{Table: t.Table(), ID: t.Int("id").PrimaryKey()}
})

type Leaf struct {
	ID         int
	OrphanID   int
	MistypedID int
}

type LeafModel struct {
	orm.Table[Leaf]
	ID         *orm.IntColumn
	OrphanID   *orm.IntColumn
	MistypedID *orm.IntColumn
}

var Leaves = orm.DefineTable[Leaf]("leaves", func(t *orm.TableBuilder[Leaf]) *LeafModel {
	return &LeafModel{
		Table:      t.Table(),
		ID:         t.Int("id").PrimaryKey(),
		OrphanID:   t.Int("orphan_id").NotNull().References(Orphans.ID),
		MistypedID: t.Int("mistyped_id").NotNull().References(Mistypeds.ID),
	}
})
