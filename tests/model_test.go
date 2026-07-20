package orm_test

import (
	"testing"

	"github.com/tork-go/orm"
)

// The types and vars below are the target model-declaration API from the
// project's design, reproduced verbatim as test fixtures. Compiling this
// file is itself a proof that the exported API supports the intended
// developer-facing usage, including NewForeignKey's type inference from
// User.ID.

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

func TestModel_TableNames(t *testing.T) {
	if got, want := User.TableName(), "users"; got != want {
		t.Errorf("User.TableName() = %q, want %q", got, want)
	}
	if got, want := Post.TableName(), "posts"; got != want {
		t.Errorf("Post.TableName() = %q, want %q", got, want)
	}
}

func TestModel_UserColumns(t *testing.T) {
	if !User.ID.IsPrimaryKey() {
		t.Error("User.ID.IsPrimaryKey() = false, want true")
	}
	if User.ID.IsNullable() {
		t.Error("User.ID.IsNullable() = true, want false")
	}

	if !User.Username.IsUnique() {
		t.Error("User.Username.IsUnique() = false, want true")
	}
	if !User.Username.IsNotNull() {
		t.Error("User.Username.IsNotNull() = false, want true")
	}
	if n, ok := User.Username.MaxLength(); !ok || n != 30 {
		t.Errorf("User.Username.MaxLength() = (%d, %v), want (30, true)", n, ok)
	}
	if User.Username.IsNullable() {
		t.Error("User.Username.IsNullable() = true, want false")
	}

	if !User.Email.IsNullable() {
		t.Error("User.Email.IsNullable() = false, want true (Column[*string])")
	}
	if User.Email.IsPrimaryKey() || User.Email.IsUnique() || User.Email.IsNotNull() {
		t.Error("User.Email has an unexpected constraint set")
	}
}

func TestModel_PostColumns(t *testing.T) {
	if !Post.ID.IsPrimaryKey() {
		t.Error("Post.ID.IsPrimaryKey() = false, want true")
	}

	if !Post.Title.IsNotNull() {
		t.Error("Post.Title.IsNotNull() = false, want true")
	}
	if n, ok := Post.Title.MaxLength(); !ok || n != 100 {
		t.Errorf("Post.Title.MaxLength() = (%d, %v), want (100, true)", n, ok)
	}

	if !Post.Content.IsNotNull() {
		t.Error("Post.Content.IsNotNull() = false, want true")
	}
	if _, ok := Post.Content.MaxLength(); ok {
		t.Error("Post.Content.MaxLength() has ok=true, want false (MaxLen never called)")
	}
}

func TestModel_PostAuthorForeignKey(t *testing.T) {
	if got, want := Post.AuthorID.Name(), "author_id"; got != want {
		t.Errorf("Post.AuthorID.Name() = %q, want %q", got, want)
	}
	if got, want := Post.AuthorID.ReferencedTable(), "users"; got != want {
		t.Errorf("Post.AuthorID.ReferencedTable() = %q, want %q", got, want)
	}
	if got, want := Post.AuthorID.ReferencedColumn(), "id"; got != want {
		t.Errorf("Post.AuthorID.ReferencedColumn() = %q, want %q", got, want)
	}
}

func TestModel_RelationshipFieldsAreZeroValue(t *testing.T) {
	if User.Posts != (orm.HasMany[PostModel]{}) {
		t.Error("User.Posts is not the zero value, want it left uninitialized")
	}
	if Post.Author != (orm.BelongsTo[UserModel]{}) {
		t.Error("Post.Author is not the zero value, want it left uninitialized")
	}
}
