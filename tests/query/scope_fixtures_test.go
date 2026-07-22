package query_test

import "github.com/tork-go/orm"

// A table with a default scope, and a HasMany into it, so scope reach into
// eager loading and Has/HasNone (query_load.go, query_compile.go) has
// something to load and check against. The soft-delete column is added
// alongside ScopedPost's other tests once SoftDelete exists.

type ScopedAuthor struct {
	ID    int
	Name  string
	Posts []ScopedPost // HasMany
}

type ScopedAuthorModel struct {
	orm.Table[ScopedAuthor]
	ID    *orm.IntColumn
	Name  *orm.StringColumn
	Posts orm.HasMany[ScopedPost]
}

var ScopedAuthors = orm.DefineTable[ScopedAuthor]("scoped_authors",
	func(t *orm.TableBuilder[ScopedAuthor]) *ScopedAuthorModel {
		return &ScopedAuthorModel{
			Table: t.Table(),
			ID:    t.Int("id").PrimaryKey(),
			Name:  t.String("name").NotNull(),
		}
	})

type ScopedPost struct {
	ID        int
	AuthorID  int
	Title     string
	Published bool
}

type ScopedPostModel struct {
	orm.Table[ScopedPost]
	ID        *orm.IntColumn
	AuthorID  *orm.IntColumn
	Title     *orm.StringColumn
	Published *orm.BoolColumn
	Author    orm.BelongsTo[ScopedAuthor]
}

// DefaultScope keeps only published posts visible by default.
func (m *ScopedPostModel) DefaultScope() orm.Predicate {
	return m.Published.Eq(true)
}

var ScopedPosts = orm.DefineTable[ScopedPost]("scoped_posts",
	func(t *orm.TableBuilder[ScopedPost]) *ScopedPostModel {
		return &ScopedPostModel{
			Table:     t.Table(),
			ID:        t.Int("id").PrimaryKey(),
			AuthorID:  t.Int("author_id").NotNull().References(ScopedAuthors.ID),
			Title:     t.String("title").NotNull(),
			Published: t.Bool("published").NotNull(),
		}
	})
