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

func TestUpdateOnly_WritesNamedColumnsOnly(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	u := &User{ID: 3, Username: "hasan", Age: 15, Email: nil}
	if err := Users.With(db).UpdateOnly(context.Background(), u, Users.Username, Users.Age); err != nil {
		t.Fatalf("UpdateOnly() error = %v", err)
	}
	got := c.ExecCalls()[0]
	want := `UPDATE "users" SET "username" = $1, "age" = $2 WHERE "id" = $3`
	if got != want {
		t.Errorf("UpdateOnly ran  %s\nwant            %s", got, want)
	}
	if args := c.ExecArgs(0); len(args) != 3 || args[0] != "hasan" || args[1] != 15 || args[2] != 3 {
		t.Errorf("UpdateOnly bound %v, want [hasan 15 3]", args)
	}
}

func TestUpdateOnly_SingleColumn(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	u := &User{ID: 3, Age: 20}
	if err := Users.With(db).UpdateOnly(context.Background(), u, Users.Age); err != nil {
		t.Fatalf("UpdateOnly() error = %v", err)
	}
	want := `UPDATE "users" SET "age" = $1 WHERE "id" = $2`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateOnly ran  %s\nwant            %s", got, want)
	}
}

func TestUpdateOnly_NoColumns(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	err := Users.With(db).UpdateOnly(context.Background(), &User{ID: 3})
	if err == nil {
		t.Fatal("UpdateOnly() error = nil, want no columns rejected")
	}
	if !strings.Contains(err.Error(), "no columns to write") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestUpdateOnly_NilColumn(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	err := Users.With(db).UpdateOnly(context.Background(), &User{ID: 3}, Users.Username, nil)
	if err == nil {
		t.Fatal("UpdateOnly() error = nil, want the nil column rejected")
	}
	if !strings.Contains(err.Error(), "column 1 is nil") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestUpdateOnly_RejectsPrimaryKey(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	err := Users.With(db).UpdateOnly(context.Background(), &User{ID: 3}, Users.ID)
	if err == nil {
		t.Fatal("UpdateOnly() error = nil, want the primary key rejected")
	}
	if !strings.Contains(err.Error(), `"id"`) || !strings.Contains(err.Error(), "primary") {
		t.Errorf("error %q does not name the primary key", err)
	}
}

func TestUpdateOnly_NoSuchRow(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	err := Users.With(db).UpdateOnly(context.Background(), &User{ID: 3}, Users.Username)
	if !errors.Is(err, orm.ErrNoRows) {
		t.Errorf("UpdateOnly() error = %v, want ErrNoRows", err)
	}
}

func TestUpdateOnly_NilRow(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	err := Users.With(db).UpdateOnly(context.Background(), nil, Users.Username)
	if err == nil {
		t.Fatal("UpdateOnly() error = nil, want the nil row rejected")
	}
}

// UpdateOnly fires the same hooks Update does.
func TestUpdateOnly_HooksFire(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	p := &Post{ID: 1, Title: "hello"}
	if err := Posts.With(db).UpdateOnly(context.Background(), p, Posts.Title); err != nil {
		t.Fatalf("UpdateOnly() error = %v", err)
	}
	if strings.Join(p.fired, ",") != "BeforeUpdate,AfterUpdate" {
		t.Errorf("fired = %v, want [BeforeUpdate AfterUpdate]", p.fired)
	}
}

// A BeforeUpdate hook that refuses stops UpdateOnly before any SQL runs,
// the same as it does for Update.
func TestUpdateOnly_BeforeUpdateHookError(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	err := Refusing.With(db).UpdateOnly(context.Background(), &refusing{ID: 1, Name: "refuse"}, Refusing.Name)
	if !errors.Is(err, errRefused) {
		t.Fatalf("UpdateOnly() error = %v, want the hook's error", err)
	}
	if len(c.ExecCalls()) != 0 {
		t.Errorf("ran %v, want no statement once the hook refused", c.ExecCalls())
	}
}

// The driver's own failure reaches the caller the same way Update's does.
func TestUpdateOnly_ExecFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailOn(`UPDATE "users" SET "username" = $1 WHERE "id" = $2`)
	db := orm.NewDB(c, postgres.Dialect{})

	err := Users.With(db).UpdateOnly(context.Background(), &User{ID: 3}, Users.Username)
	if err == nil {
		t.Fatal("UpdateOnly() error = nil, want the driver's failure")
	}
	if !strings.Contains(err.Error(), "updating") {
		t.Errorf("error %q does not say what failed", err)
	}
}
