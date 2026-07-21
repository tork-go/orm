// Package fixtures holds shared model definitions used across the
// migrations tests (schema extraction, diffing, DDL rendering, and the
// CLI), so every package's tests exercise the same models instead of each
// redefining their own.
//
// The naming follows the convention the docs describe: the singular name
// is the row type (User), the plural is the table (Users). That keeps
// User{Username: "alice"} and Users.Query(db) unambiguous at a glance.
package fixtures

import "github.com/tork-go/orm"

// User is the row type for the users table.
type User struct {
	ID       int
	Username string
	Email    *string

	// Posts is not a column. Fields matching no column are ignored by the
	// entity mapping, which is what lets related rows live on the row type.
	Posts []Post
}

type UserModel struct {
	orm.Table[User]
	ID       *orm.IntColumn
	Username *orm.StringColumn
	Email    *orm.NullableStringColumn
	Posts    orm.HasMany[Post]
}

var Users = orm.DefineTable[User]("users", func(t *orm.TableBuilder[User]) *UserModel {
	return &UserModel{
		Table:    t.Table(),
		ID:       t.Int("id").PrimaryKey(),
		Username: t.String("username").Unique().NotNull().MaxLen(30),
		Email:    t.NullableString("email"),
	}
})

// Post is the row type for the posts table.
type Post struct {
	ID       int
	Title    string
	Content  string
	AuthorID int
}

type PostModel struct {
	orm.Table[Post]
	ID       *orm.IntColumn
	Title    *orm.StringColumn
	Content  *orm.StringColumn
	AuthorID *orm.IntColumn
	Author   orm.BelongsTo[User]
}

var Posts = orm.DefineTable[Post]("posts", func(t *orm.TableBuilder[Post]) *PostModel {
	return &PostModel{
		Table:   t.Table(),
		ID:      t.Int("id").PrimaryKey(),
		Title:   t.String("title").NotNull().MaxLen(100),
		Content: t.String("content").NotNull(),
		// A foreign key is a column that references another. The target
		// carries its own table name, so nothing repeats "users" here, and
		// the constraint name is derived rather than spelled out.
		AuthorID: t.Int("author_id").NotNull().References(Users.ID),
	}
})
