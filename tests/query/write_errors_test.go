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

// The guards every write shares, exercised once each through one operation
// rather than repeated across all four.
func TestWrite_Guards(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	t.Run("nil row", func(t *testing.T) {
		if err := Users.With(db).Insert(context.Background(), nil); err == nil {
			t.Error("Insert(nil) produced no error")
		}
	})

	t.Run("no handle", func(t *testing.T) {
		err := Users.With(nil).Insert(context.Background(), &User{})
		if err == nil || !strings.Contains(err.Error(), "With") {
			t.Errorf("error = %v, want it to point at With", err)
		}
	})

	t.Run("zero table", func(t *testing.T) {
		var tbl orm.Table[User]
		if err := tbl.With(db).Insert(context.Background(), &User{}); err == nil {
			t.Error("Insert on a zero table produced no error")
		}
	})

	t.Run("no entity mapping", func(t *testing.T) {
		type model struct {
			orm.Table[orm.NoEntity]
			ID *orm.IntColumn
		}
		m := &model{Table: orm.NewTable[orm.NoEntity]("legacy"), ID: orm.NewIntColumn("id")}
		err := m.Table.With(db).Insert(context.Background(), &struct{}{})
		if err == nil || !strings.Contains(err.Error(), "DefineTable") {
			t.Errorf("error = %v, want it to point at DefineTable", err)
		}
	})
}

// Every write reaches the guards, so each is worth one call to prove it.
func TestWrite_GuardsOnEveryOperation(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	for name, run := range map[string]func() error{
		"Insert": func() error { return Users.With(db).Insert(context.Background(), nil) },
		"Update": func() error { return Users.With(db).Update(context.Background(), nil) },
		"Delete": func() error { return Users.With(db).Delete(context.Background(), nil) },
		"Save":   func() error { return Users.With(db).Save(context.Background(), nil) },
	} {
		if err := run(); err == nil {
			t.Errorf("%s(nil) produced no error", name)
		}
	}
}

// A table with no primary key cannot say which row a write means.
func TestWrite_WithoutAPrimaryKey(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	err := Events.With(db).Update(context.Background(), &Event{Name: "x"})
	if err == nil {
		t.Fatal("Update() error = nil, want a missing key error")
	}
	if !strings.Contains(err.Error(), "declares none") {
		t.Errorf("error %q does not report the missing key", err)
	}
	if err := Events.With(db).Delete(context.Background(), &Event{}); err == nil {
		t.Error("Delete() produced no error")
	}
}

// A table whose every column is part of the key has nothing to write.
func TestUpdate_NothingToSet(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	err := Memberships.With(db).Update(context.Background(), &Membership{OrgID: 1, UserID: 2})
	if err == nil {
		t.Fatal("Update() error = nil, want it to report an empty SET")
	}
	if !strings.Contains(err.Error(), "nothing to write") {
		t.Errorf("error %q does not report the empty SET", err)
	}
}

// A composite key filters on every one of its columns.
func TestDelete_CompositeKey(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if err := Memberships.With(db).Delete(context.Background(), &Membership{OrgID: 1, UserID: 2}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	got := c.ExecCalls()[0]
	want := `DELETE FROM "memberships" WHERE ("org_id" = $1 AND "user_id" = $2)`
	if got != want {
		t.Errorf("Delete ran %s\nwant       %s", got, want)
	}
}

// A row where the database supplies every column still has to be
// insertable.
type allDefault struct{ ID int }

type allDefaultModel struct {
	orm.Table[allDefault]
	ID *orm.IntColumn
}

var AllDefault = orm.DefineTable[allDefault]("all_default", func(t *orm.TableBuilder[allDefault]) *allDefaultModel {
	return &allDefaultModel{Table: t.Table(), ID: t.Int("id").PrimaryKey()}
})

func TestInsert_EveryColumnGenerated(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{4})
	db := orm.NewDB(c, postgres.Dialect{})

	a := &allDefault{}
	if err := AllDefault.With(db).Insert(context.Background(), a); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	got := c.QueryCalls()[0]
	want := `INSERT INTO "all_default" DEFAULT VALUES RETURNING "id"`
	if got != want {
		t.Errorf("Insert ran %s\nwant       %s", got, want)
	}
	if a.ID != 4 {
		t.Errorf("ID = %d, want 4", a.ID)
	}
}

func TestWrite_ReportsDriverFailures(t *testing.T) {
	t.Run("insert", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.FailOn(`INSERT INTO "users" ("username", "email", "age", "prefs", "created_at")` +
			` VALUES ($1, $2, $3, $4, $5) RETURNING "id"`)
		db := orm.NewDB(c, postgres.Dialect{})
		if err := Users.With(db).Insert(context.Background(), &User{}); err == nil {
			t.Error("Insert() produced no error")
		}
	})

	t.Run("insert without returning", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.FailOn(`INSERT INTO [keyed] ([id], [name]) VALUES (?, ?)`)
		db := orm.NewDB(c, fakedriver.NewDialect())
		if err := Keyed.With(db).Insert(context.Background(), &keyed{Name: "x"}); err == nil {
			t.Error("Insert() produced no error")
		}
	})

	t.Run("update", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.FailOn(`UPDATE "users" SET "username" = $1, "email" = $2, "age" = $3, ` +
			`"prefs" = $4, "created_at" = $5 WHERE "id" = $6`)
		db := orm.NewDB(c, postgres.Dialect{})
		if err := Users.With(db).Update(context.Background(), &User{ID: 1}); err == nil {
			t.Error("Update() produced no error")
		}
	})

	t.Run("delete", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.FailOn(`DELETE FROM "users" WHERE "id" = $1`)
		db := orm.NewDB(c, postgres.Dialect{})
		if err := Users.With(db).Delete(context.Background(), &User{ID: 1}); err == nil {
			t.Error("Delete() produced no error")
		}
	})
}

// An insert that returns nothing cannot fill the row back in.
func TestInsert_ReturningNoRow(t *testing.T) {
	c := fakedriver.NewConn() // nothing queued, so RETURNING yields no row
	db := orm.NewDB(c, postgres.Dialect{})
	err := Users.With(db).Insert(context.Background(), &User{})
	if err == nil {
		t.Fatal("Insert() error = nil, want it to report the empty result")
	}
	if !strings.Contains(err.Error(), "returned no row") {
		t.Errorf("error %q does not report the empty result", err)
	}
}

func TestInsert_ReturningRowsError(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsErr = errTruncated
	db := orm.NewDB(c, postgres.Dialect{})
	if err := Users.With(db).Insert(context.Background(), &User{}); err == nil {
		t.Error("Insert() produced no error")
	}
}

// A returned value that does not fit the field is a scan failure, named as
// one.
func TestInsert_ReturningScanFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"not an int"})
	db := orm.NewDB(c, postgres.Dialect{})
	err := Users.With(db).Insert(context.Background(), &User{})
	if err == nil {
		t.Fatal("Insert() error = nil, want a scan failure")
	}
	if !strings.Contains(err.Error(), "reading back") {
		t.Errorf("error %q does not report the read back", err)
	}
}

// A document column left to the database is decoded on the way back, the
// same as any other read.
func TestInsert_ReturningDecodesDocuments(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{9, []byte(`{"theme":"dark"}`)})
	db := orm.NewDB(c, postgres.Dialect{})

	d := &docDefault{Name: "x"}
	if err := DocDefault.With(db).Insert(context.Background(), d); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if d.Prefs.Theme != "dark" {
		t.Errorf("Prefs = %+v, want the decoded document", d.Prefs)
	}
}

type docDefault struct {
	ID    int
	Name  string
	Prefs Prefs
}

type docDefaultModel struct {
	orm.Table[docDefault]
	ID    *orm.IntColumn
	Name  *orm.StringColumn
	Prefs *orm.JSONColumn[Prefs]
}

var DocDefault = orm.DefineTable[docDefault]("doc_default", func(t *orm.TableBuilder[docDefault]) *docDefaultModel {
	return &docDefaultModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
		Prefs: orm.NewJSONColumn[Prefs]("prefs").ServerDefault(`'{}'`),
	}
})

// A custom codec can fail, and a write has to report that rather than send
// the driver a value it could not encode.
type badDoc struct {
	ID    int
	Name  string
	Prefs Prefs
}

type badDocModel struct {
	orm.Table[badDoc]
	ID    *orm.IntColumn
	Name  *orm.StringColumn
	Prefs *orm.JSONColumn[Prefs]
}

var errCannotEncode = errors.New("codec refuses to encode this")

var BadDoc = orm.DefineTable[badDoc]("bad_doc", func(t *orm.TableBuilder[badDoc]) *badDocModel {
	return &badDocModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
		Prefs: orm.NewJSONColumn[Prefs]("prefs").Serialize(
			func(Prefs) ([]byte, error) { return nil, errCannotEncode },
			func([]byte) (Prefs, error) { return Prefs{}, nil },
		),
	}
})

func TestWrite_EncodeFailureIsReported(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	t.Run("insert", func(t *testing.T) {
		err := BadDoc.With(db).Insert(context.Background(), &badDoc{Name: "x"})
		if !errors.Is(err, errCannotEncode) {
			t.Errorf("Insert() error = %v, want it to wrap the codec's own error", err)
		}
	})

	t.Run("update", func(t *testing.T) {
		err := BadDoc.With(db).Update(context.Background(), &badDoc{ID: 1, Name: "x"})
		if !errors.Is(err, errCannotEncode) {
			t.Errorf("Update() error = %v, want it to wrap the codec's own error", err)
		}
	})
}

// Reading a written row back can fail the same way an ordinary scan can.
func TestInsert_ReadBackDecodeFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{9, []byte("not json")})
	db := orm.NewDB(c, postgres.Dialect{})

	err := DocDefault.With(db).Insert(context.Background(), &docDefault{Name: "x"})
	if err == nil {
		t.Fatal("Insert() error = nil, want a decode failure")
	}
	if !strings.Contains(err.Error(), `column "prefs"`) {
		t.Errorf("error %q does not name the column", err)
	}
}
