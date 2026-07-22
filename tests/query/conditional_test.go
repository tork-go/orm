package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

func TestUpdateIf_Statement(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	u := &User{ID: 7, Username: "alice", Age: 31}
	err := Users.With(db).UpdateIf(context.Background(), u, Users.Age.Equals(30))
	if err != nil {
		t.Fatalf("UpdateIf() error = %v", err)
	}

	// The key and the condition are one WHERE, joined with AND: this row, and
	// only while it still looks like this.
	want := `UPDATE "users" SET "username" = $1, "email" = $2, "age" = $3, ` +
		`"prefs" = $4, "created_at" = $5 WHERE ("id" = $6 AND "age" = $7)`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("ran  %s\nwant %s", got, want)
	}
	args := c.ExecArgs(0)
	if len(args) != 7 || args[5] != 7 || args[6] != 30 {
		t.Errorf("bound %v, want the key then the condition", args)
	}
}

func TestDeleteIf_Statement(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	p := &Post{ID: 3, Title: "draft"}
	if err := Posts.With(db).DeleteIf(context.Background(), p, Posts.Slug.Equals("draft")); err != nil {
		t.Fatalf("DeleteIf() error = %v", err)
	}
	want := `DELETE FROM "posts" WHERE ("id" = $1 AND "slug" = $2)`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("ran  %s\nwant %s", got, want)
	}
}

// Several conditions are joined with AND, as they are anywhere else.
func TestUpdateIf_SeveralConditions(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	err := Users.With(db).UpdateIf(context.Background(), &User{ID: 1},
		Users.Age.Equals(30), Users.Username.Equals("alice"))
	if err != nil {
		t.Fatalf("UpdateIf() error = %v", err)
	}
	if got := c.ExecCalls()[0]; !strings.HasSuffix(got,
		`WHERE ("id" = $6 AND "age" = $7 AND "username" = $8)`) {
		t.Errorf("ran %s, want both conditions", got)
	}
}

// Any predicate is a condition, so the whole vocabulary is available.
func TestUpdateIf_TakesAnyPredicate(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	err := Users.With(db).UpdateIf(context.Background(), &User{ID: 1},
		orm.Or(Users.Age.LessThan(18), Users.Email.IsNull()))
	if err != nil {
		t.Fatalf("UpdateIf() error = %v", err)
	}
	if got := c.ExecCalls()[0]; !strings.Contains(got, `("age" < $7 OR "email" IS NULL)`) {
		t.Errorf("ran %s, want the alternatives", got)
	}
}

// A write that touched no row means the row is gone or the condition no
// longer holds, and says both rather than picking one.
func TestConditional_NoRowIsErrNoRows(t *testing.T) {
	tests := map[string]struct {
		run  func(*orm.DB) error
		want string
	}{
		"UpdateIf": {
			run: func(db *orm.DB) error {
				return Users.With(db).UpdateIf(context.Background(),
					&User{ID: 1}, Users.Age.Equals(30))
			},
			want: "UpdateIf written no row",
		},
		"DeleteIf": {
			run: func(db *orm.DB) error {
				return Users.With(db).DeleteIf(context.Background(),
					&User{ID: 1}, Users.Age.Equals(30))
			},
			want: "DeleteIf removed no row",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := fakedriver.NewConn() // RowsAffected stays 0
			db := orm.NewDB(c, postgres.Dialect{})

			err := tt.run(db)
			if !errors.Is(err, orm.ErrNoRows) {
				t.Fatalf("error = %v, want it to wrap ErrNoRows", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not name the operation", err)
			}
			if !strings.Contains(err.Error(), "conditions no longer hold") {
				t.Errorf("error %q does not say what else it could mean", err)
			}
		})
	}
}

// The hooks the unconditional forms run still run.
func TestConditional_RunsTheWriteHooks(t *testing.T) {
	t.Run("update", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.RowsAffected = 1
		db := orm.NewDB(c, postgres.Dialect{})

		p := &Post{ID: 1, Title: "Hello There"}
		if err := Posts.With(db).UpdateIf(context.Background(), p,
			Posts.Slug.Equals("old")); err != nil {
			t.Fatalf("UpdateIf() error = %v", err)
		}
		if p.Slug != "hello-there" {
			t.Errorf("slug = %q, want the one BeforeUpdate derived", p.Slug)
		}
		if strings.Join(p.fired, ",") != "BeforeUpdate,AfterUpdate" {
			t.Errorf("fired %v, want both update hooks", p.fired)
		}
	})

	t.Run("delete", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.RowsAffected = 1
		db := orm.NewDB(c, postgres.Dialect{})

		p := &Post{ID: 1}
		if err := Posts.With(db).DeleteIf(context.Background(), p,
			Posts.Slug.Equals("x")); err != nil {
			t.Fatalf("DeleteIf() error = %v", err)
		}
		if strings.Join(p.fired, ",") != "BeforeDelete,AfterDelete" {
			t.Errorf("fired %v, want both delete hooks", p.fired)
		}
	})
}

// A Before hook that refuses stops the write before any SQL runs, which is
// what makes it usable for validation here as anywhere else.
func TestConditional_ARefusingHookStopsTheWrite(t *testing.T) {
	tests := map[string]func(*orm.DB) error{
		"update": func(db *orm.DB) error {
			return Refusing.With(db).UpdateIf(context.Background(),
				&refusing{ID: 1, Name: "refuse"}, Refusing.Name.Equals("x"))
		},
		"delete": func(db *orm.DB) error {
			return Refusing.With(db).DeleteIf(context.Background(),
				&refusing{ID: 1, Name: "refuse"}, Refusing.Name.Equals("x"))
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			c := fakedriver.NewConn()
			c.RowsAffected = 1
			db := orm.NewDB(c, postgres.Dialect{})

			if err := run(db); !errors.Is(err, errRefused) {
				t.Fatalf("error = %v, want the hook's refusal", err)
			}
			if len(c.ExecCalls()) != 0 {
				t.Errorf("ran %v, want nothing written", c.ExecCalls())
			}
		})
	}
}

// Without a condition it is Update, which already exists and says so.
func TestConditional_NeedsACondition(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	err := Users.With(db).UpdateIf(context.Background(), &User{ID: 1})
	if err == nil || !strings.Contains(err.Error(), "call Update") {
		t.Errorf("UpdateIf() error = %v, want it to point at Update", err)
	}

	err = Users.With(db).DeleteIf(context.Background(), &User{ID: 1})
	if err == nil || !strings.Contains(err.Error(), "call Delete") {
		t.Errorf("DeleteIf() error = %v, want it to point at Delete", err)
	}
}

// The query's own problems and the row's are reported the same way the
// unconditional forms report them.
func TestConditional_Rejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	t.Run("a nil row", func(t *testing.T) {
		err := Users.With(db).UpdateIf(context.Background(), nil, Users.Age.Equals(1))
		if err == nil || !strings.Contains(err.Error(), "nil row") {
			t.Errorf("error = %v, want the nil row reported", err)
		}
	})

	t.Run("a table with no primary key", func(t *testing.T) {
		err := Events.With(db).UpdateIf(context.Background(), &Event{Name: "x"},
			Events.Name.Equals("x"))
		if err == nil || !strings.Contains(err.Error(), "needs a primary key") {
			t.Errorf("error = %v, want the missing key reported", err)
		}
	})

	t.Run("another table's column in the condition", func(t *testing.T) {
		err := Users.With(db).UpdateIf(context.Background(), &User{ID: 1},
			Posts.Title.Equals("x"))
		if err == nil || !strings.Contains(err.Error(), `belongs to table "posts"`) {
			t.Errorf("UpdateIf() error = %v, want the foreign column rejected", err)
		}

		err = Users.With(db).DeleteIf(context.Background(), &User{ID: 1},
			Posts.Title.Equals("x"))
		if err == nil || !strings.Contains(err.Error(), `belongs to table "posts"`) {
			t.Errorf("DeleteIf() error = %v, want the foreign column rejected", err)
		}
	})

	t.Run("no database handle", func(t *testing.T) {
		var none *orm.DB
		err := Users.With(none).DeleteIf(context.Background(), &User{ID: 1}, Users.Age.Equals(1))
		if err == nil || !strings.Contains(err.Error(), "no database handle") {
			t.Errorf("error = %v, want the missing handle reported", err)
		}
	})
}

// Nothing about it may assume Postgres's spelling.
func TestConditional_AsksTheDialect(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, fakedriver.NewDialect())

	if err := Memberships.With(db).DeleteIf(context.Background(),
		&Membership{OrgID: 1, UserID: 2}, Memberships.UserID.GreaterThan(0)); err != nil {
		t.Fatalf("DeleteIf() error = %v", err)
	}
	want := `DELETE FROM [memberships] WHERE ([org_id] = ? AND [user_id] = ? AND [user_id] > ?)`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("ran  %s\nwant %s", got, want)
	}
}
