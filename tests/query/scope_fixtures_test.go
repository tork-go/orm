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

	Tags []ScopedTag // ManyToMany, through ScopedPostTags
}

type ScopedPostModel struct {
	orm.Table[ScopedPost]
	ID        *orm.IntColumn
	AuthorID  *orm.IntColumn
	Title     *orm.StringColumn
	Published *orm.BoolColumn
	Author    orm.BelongsTo[ScopedAuthor]
	Tags      orm.ManyToMany[ScopedTag]
}

// DefaultScope keeps only published posts visible by default.
func (m *ScopedPostModel) DefaultScope() orm.Predicate {
	return m.Published.Eq(true)
}

// The join table is named here rather than inferred, the same as BookModel
// names BookTags: nothing about a ManyToMany[ScopedTag] says which table
// joins the two.
func (m *ScopedPostModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{orm.Through(&m.Tags, ScopedPostTags.PostID, ScopedPostTags.TagID)}
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

// ScopedTag has its own default scope, so Has and HasNone over a many to
// many relationship (query_compile.go's existsThrough) has a far table
// whose scope reach can be tested independently of the join table's.
type ScopedTag struct {
	ID     int
	Name   string
	Active bool
}

type ScopedTagModel struct {
	orm.Table[ScopedTag]
	ID     *orm.IntColumn
	Name   *orm.StringColumn
	Active *orm.BoolColumn
}

func (m *ScopedTagModel) DefaultScope() orm.Predicate {
	return m.Active.Eq(true)
}

var ScopedTags = orm.DefineTable[ScopedTag]("scoped_tags", func(t *orm.TableBuilder[ScopedTag]) *ScopedTagModel {
	return &ScopedTagModel{
		Table:  t.Table(),
		ID:     t.Int("id").PrimaryKey(),
		Name:   t.String("name").NotNull(),
		Active: t.Bool("active").NotNull(),
	}
})

type ScopedPostTag struct {
	PostID int
	TagID  int
}

type ScopedPostTagModel struct {
	orm.Table[ScopedPostTag]
	PostID *orm.IntColumn
	TagID  *orm.IntColumn
}

var ScopedPostTags = orm.DefineTable[ScopedPostTag]("scoped_post_tags",
	func(t *orm.TableBuilder[ScopedPostTag]) *ScopedPostTagModel {
		return &ScopedPostTagModel{
			Table:  t.Table(),
			PostID: t.Int("post_id").PrimaryKey().References(ScopedPosts.ID),
			TagID:  t.Int("tag_id").PrimaryKey().References(ScopedTags.ID),
		}
	})
