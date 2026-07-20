// Package fixtures holds shared model definitions used across the round 2
// migrations tests (schema extraction, diffing, DDL rendering, and the
// CLI), so every package's tests exercise the same models instead of each
// redefining their own.
package fixtures

import "github.com/tork-go/orm"

type UserModel struct {
	orm.Table
	ID       *orm.Column[int]
	Username *orm.Column[string]
	Email    *orm.Column[*string]
	Posts    orm.HasMany[PostModel]
}

var User = &UserModel{
	Table:    orm.NewTable("users"),
	ID:       orm.NewColumn[int]("id").PrimaryKey(),
	Username: orm.NewColumn[string]("username").Unique().NotNull().MaxLen(30),
	Email:    orm.NewColumn[*string]("email"),
}

type PostModel struct {
	orm.Table
	ID       *orm.Column[int]
	Title    *orm.Column[string]
	Content  *orm.Column[string]
	AuthorID *orm.ForeignKey[int]
	Author   orm.BelongsTo[UserModel]
}

var Post = &PostModel{
	Table:    orm.NewTable("posts"),
	ID:       orm.NewColumn[int]("id").PrimaryKey(),
	Title:    orm.NewColumn[string]("title").NotNull().MaxLen(100),
	Content:  orm.NewColumn[string]("content").NotNull(),
	AuthorID: orm.NewForeignKey("author_id", User.TableName(), User.ID),
}
