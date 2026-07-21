package query_test

import (
	"time"

	"github.com/tork-go/orm"
)

// Prefs is a document column's Go type, so the encode and decode paths are
// exercised by an ordinary query rather than only by a scan test.
type Prefs struct {
	Theme string `json:"theme"`
}

type User struct {
	ID        int
	Username  string
	Email     *string
	Age       int
	Prefs     Prefs
	CreatedAt time.Time
}

type UserModel struct {
	orm.Table[User]
	ID        *orm.IntColumn
	Username  *orm.StringColumn
	Email     *orm.NullableStringColumn
	Age       *orm.IntColumn
	Prefs     *orm.JSONColumn[Prefs]
	CreatedAt *orm.TimeColumn
}

var Users = orm.DefineTable[User]("users", func(t *orm.TableBuilder[User]) *UserModel {
	return &UserModel{
		Table:     t.Table(),
		ID:        t.Int("id").PrimaryKey(),
		Username:  t.String("username").NotNull().MaxLen(30),
		Email:     t.NullableString("email"),
		Age:       t.Int("age").NotNull(),
		Prefs:     orm.NewJSONColumn[Prefs]("prefs"),
		CreatedAt: t.Time("created_at").NotNull(),
	}
})

// Post is declared in hooks_test.go, where it carries the lifecycle hooks.
// It doubles as the second table the compiler tests need, so a predicate
// can name a column the users table does not own.

// A composite primary key, so Find can report that it has no single key to
// look up by.
type Membership struct {
	OrgID  int
	UserID int
}

type MembershipModel struct {
	orm.Table[Membership]
	OrgID  *orm.IntColumn
	UserID *orm.IntColumn
}

var Memberships = orm.DefineTable[Membership]("memberships",
	func(t *orm.TableBuilder[Membership]) *MembershipModel {
		return &MembershipModel{
			Table:  t.Table(),
			OrgID:  t.Int("org_id").PrimaryKey(),
			UserID: t.Int("user_id").PrimaryKey(),
		}
	})

// A table with no primary key at all.
type Event struct{ Name string }

type EventModel struct {
	orm.Table[Event]
	Name *orm.StringColumn
}

var Events = orm.DefineTable[Event]("events", func(t *orm.TableBuilder[Event]) *EventModel {
	return &EventModel{Table: t.Table(), Name: t.String("name").NotNull()}
})

// userCols is the SELECT list every expected statement starts with, in the
// Postgres spelling. Written once so a column added to the fixture does
// not have to be threaded through every test.
const userCols = `"id", "username", "email", "age", "prefs", "created_at"`
