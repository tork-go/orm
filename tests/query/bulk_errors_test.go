package query_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// A bad row is found by its position, since "nil row" says nothing about
// which of five hundred it was.
func TestBulk_NilRowIsReportedByPosition(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	ctx := context.Background()

	tests := map[string]func() error{
		"InsertMany": func() error {
			return Users.With(db).InsertMany(ctx, &User{Username: "a"}, nil, &User{Username: "c"})
		},
		"UpdateMany": func() error {
			_, err := Users.With(db).UpdateMany(ctx, &User{ID: 1}, nil)
			return err
		},
		"DeleteMany": func() error {
			_, err := Users.With(db).DeleteMany(ctx, &User{ID: 1}, nil)
			return err
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			err := run()
			if err == nil {
				t.Fatal("no error, want the nil row reported")
			}
			if !strings.Contains(err.Error(), "row 1 is nil") {
				t.Errorf("error %q does not say which row was nil", err)
			}
		})
	}
}

// The checks that are about the query rather than any one row are made once,
// however many rows were passed.
func TestBulk_QueryLevelFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("no entity mapping", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		err := unmapped().With(db).InsertMany(ctx, &struct{}{})
		if err == nil {
			t.Fatal("InsertMany() error = nil, want the missing mapping reported")
		}
		if !strings.Contains(err.Error(), "no entity mapping") {
			t.Errorf("error %q does not name the problem", err)
		}
	})

	t.Run("no database handle", func(t *testing.T) {
		err := Users.With(nil).InsertMany(ctx, &User{Username: "a"})
		if err == nil {
			t.Fatal("InsertMany() error = nil, want the missing handle reported")
		}
		if !strings.Contains(err.Error(), "no database handle") {
			t.Errorf("error %q does not name the problem", err)
		}
	})
}

// A table with no primary key has nothing to identify its rows by, so a
// batch delete says so and points at the operation that does work.
func TestDeleteMany_NeedsAPrimaryKey(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Events.With(db).DeleteMany(context.Background(), &Event{Name: "x"})
	if err == nil {
		t.Fatal("DeleteMany() error = nil, want the missing key reported")
	}
	for _, want := range []string{"needs a primary key", "DeleteAll"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// A Before hook that fails stops the batch before any SQL runs, including
// for the rows whose own hooks had already succeeded.
func TestBulk_BeforeHookFailureRunsNoStatement(t *testing.T) {
	ctx := context.Background()

	tests := map[string]func(db *orm.DB, rows []*Failing) error{
		"InsertMany": func(db *orm.DB, rows []*Failing) error {
			return FailingRows.With(db).InsertMany(ctx, rows...)
		},
		"UpdateMany": func(db *orm.DB, rows []*Failing) error {
			_, err := FailingRows.With(db).UpdateMany(ctx, rows...)
			return err
		},
		"DeleteMany": func(db *orm.DB, rows []*Failing) error {
			_, err := FailingRows.With(db).DeleteMany(ctx, rows...)
			return err
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			c := fakedriver.NewConn()
			db := orm.NewDB(c, postgres.Dialect{})
			// The second row's hook fails; the first's does not.
			rows := []*Failing{{ID: 1}, {ID: 2, Fail: true}}

			err := run(db, rows)
			if !errors.Is(err, errHookRefused) {
				t.Fatalf("error = %v, want the hook's own error", err)
			}
			if n := len(c.ExecCalls()) + len(c.QueryCalls()); n != 0 {
				t.Errorf("ran %d statements after a hook refused, want none", n)
			}
		})
	}
}

// A failure partway through rolls back the statements before it, which is
// what the transaction is for.
func TestInsertMany_MidChunkFailureRollsBack(t *testing.T) {
	c := fakedriver.NewConn()
	d := fakedriver.NewDialect()
	d.BindLimit = 4 // two rows a statement
	db := orm.NewDB(c, d)

	// The second statement is the one that fails.
	c.FailOn(`INSERT INTO [memberships] ([org_id], [user_id]) VALUES (?, ?), (?, ?)`)

	rows := make([]*Membership, 4)
	for i := range rows {
		rows[i] = &Membership{OrgID: 1, UserID: i}
	}
	err := Memberships.With(db).InsertMany(context.Background(), rows...)
	if err == nil {
		t.Fatal("InsertMany() error = nil, want the driver failure")
	}
	if !strings.Contains(err.Error(), "inserting") {
		t.Errorf("error %q does not say what failed", err)
	}
	if len(c.Txs()) != 1 {
		t.Fatalf("opened %d transactions, want 1", len(c.Txs()))
	}
	if !c.Txs()[0].RolledBack {
		t.Error("the earlier chunk was left committed")
	}
	if c.Txs()[0].Committed {
		t.Error("the transaction was committed despite the failure")
	}
}

func TestUpdateMany_MidRowFailureRollsBack(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})
	c.FailOn(`UPDATE "memberships" SET "org_id" = $1, "user_id" = $2 WHERE ("org_id" = $3 AND "user_id" = $4)`)

	_, err := Memberships.With(db).UpdateMany(context.Background(),
		&Membership{OrgID: 1, UserID: 1}, &Membership{OrgID: 2, UserID: 2})
	if err == nil {
		t.Fatal("UpdateMany() error = nil, want the driver failure")
	}
	if !strings.Contains(err.Error(), "row 0") {
		t.Errorf("error %q does not say which row failed", err)
	}
	if !c.Txs()[0].RolledBack {
		t.Error("the batch was not rolled back")
	}
}

// A table whose every column is part of the key has nothing an update could
// write, which the batch reports the same way one row does, naming the row.
func TestUpdateMany_NothingToWrite(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Memberships.With(db).UpdateMany(context.Background(),
		&Membership{OrgID: 1, UserID: 1})
	if err == nil {
		t.Fatal("UpdateMany() error = nil, want it to report having nothing to write")
	}
	if !strings.Contains(err.Error(), "nothing to write") {
		t.Errorf("error %q does not name the problem", err)
	}
	if !strings.Contains(err.Error(), "(row 0)") {
		t.Errorf("error %q does not say which row", err)
	}
	// The table is named once, not once per layer of wrapping.
	if strings.Count(err.Error(), "orm: ") != 1 {
		t.Errorf("error %q says orm more than once", err)
	}
}

func TestDeleteMany_ExecFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailOn(`DELETE FROM "users" WHERE "id" IN ($1)`)
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := Users.With(db).DeleteMany(context.Background(), &User{ID: 1})
	if err == nil {
		t.Fatal("DeleteMany() error = nil, want the driver failure")
	}
	if !strings.Contains(err.Error(), "deleting") {
		t.Errorf("error %q does not say what failed", err)
	}
}

// A batch that wrote fewer rows than it was given has found rows that were
// not there, which is the same thing Update and Delete report ErrNoRows for.
// The count still comes back, so a caller can see how far it got.
func TestBulk_PartialWriteIsReported(t *testing.T) {
	ctx := context.Background()

	tests := map[string]struct {
		run  func(db *orm.DB) (int64, error)
		verb string
	}{
		"UpdateMany": {
			run: func(db *orm.DB) (int64, error) {
				return Users.With(db).UpdateMany(ctx, &User{ID: 1}, &User{ID: 2})
			},
			verb: "updated",
		},
		"DeleteMany": {
			run: func(db *orm.DB) (int64, error) {
				return Users.With(db).DeleteMany(ctx, &User{ID: 1}, &User{ID: 2})
			},
			verb: "deleted",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := fakedriver.NewConn() // RowsAffected stays zero
			n, err := tt.run(orm.NewDB(c, postgres.Dialect{}))
			if err == nil {
				t.Fatal("no error, want the shortfall reported")
			}
			if !errors.Is(err, orm.ErrNoRows) {
				t.Errorf("error %v does not wrap ErrNoRows", err)
			}
			for _, want := range []string{name, tt.verb, "0 of 2 rows"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
			if n != 0 {
				t.Errorf("count = %d, want the rows actually written", n)
			}
		})
	}
}

// A batch that wrote every row it was given reports no shortfall.
func TestBulk_FullWriteIsNotAnError(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Users.With(db).UpdateMany(context.Background(), &User{ID: 1}, &User{ID: 2})
	if err != nil {
		t.Errorf("UpdateMany() error = %v, want nil for a batch that wrote every row", err)
	}
	if n != 2 {
		t.Errorf("UpdateMany() = %d, want 2", n)
	}
}

// A statement that came back with fewer rows than it wrote cannot be matched
// up, so it is reported rather than leaving rows silently unfilled.
func TestInsertMany_TooFewReturnedRows(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1}) // one row back for two written
	db := orm.NewDB(c, postgres.Dialect{})

	err := Users.With(db).InsertMany(context.Background(),
		&User{Username: "a"}, &User{Username: "b"})
	if err == nil {
		t.Fatal("InsertMany() error = nil, want the short result reported")
	}
	if !strings.Contains(err.Error(), "wrote 2 rows but returned 1") {
		t.Errorf("error %q does not say how many were missing", err)
	}
}

// A result set that fails partway through is reported the way a real driver
// reports one, from Err rather than from Next.
func TestInsertMany_RowsError(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsErr = errors.New("connection lost")
	db := orm.NewDB(c, postgres.Dialect{})

	err := Users.With(db).InsertMany(context.Background(),
		&User{Username: "a"}, &User{Username: "b"})
	if err == nil {
		t.Fatal("InsertMany() error = nil, want the result set failure")
	}
	if !strings.Contains(err.Error(), "connection lost") {
		t.Errorf("error %q does not carry the driver's own", err)
	}
}

// A codec that refuses a value must stop the statement rather than hand the
// driver something it cannot write.
func TestInsertMany_EncodeFailureIsReported(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	err := BadDoc.With(db).InsertMany(context.Background(),
		&badDoc{Name: "a"}, &badDoc{Name: "b"})
	if !errors.Is(err, errCannotEncode) {
		t.Errorf("InsertMany() error = %v, want it to wrap the codec's own error", err)
	}
}

// An After hook that fails reports itself, and the rows stay written: that
// is what Insert does for one row, and a batch does not get a different
// contract.
func TestInsertMany_AfterHookFailureLeavesRowsWritten(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1}, []any{2})
	db := orm.NewDB(c, postgres.Dialect{})

	rows := []*Failing{{Name: "a"}, {Name: "b", FailAfter: true}}
	err := FailingRows.With(db).InsertMany(context.Background(), rows...)
	if !errors.Is(err, errHookRefused) {
		t.Fatalf("error = %v, want the hook's own error", err)
	}
	if len(c.ExecCalls())+len(c.QueryCalls()) == 0 {
		t.Error("the rows were never written, so the After hook cannot have run")
	}
	if len(c.Txs()) == 1 && c.Txs()[0].RolledBack {
		t.Error("an After hook rolled the batch back, which Insert does not do for one row")
	}
}

// An After hook on a set of rows reports itself and keeps the count, so a
// caller can see how much was written before it refused.
func TestBulk_AfterHookFailureKeepsTheCount(t *testing.T) {
	ctx := context.Background()

	tests := map[string]func(db *orm.DB, rows []*Failing) (int64, error){
		"UpdateMany": func(db *orm.DB, rows []*Failing) (int64, error) {
			return FailingRows.With(db).UpdateMany(ctx, rows...)
		},
		"DeleteMany": func(db *orm.DB, rows []*Failing) (int64, error) {
			return FailingRows.With(db).DeleteMany(ctx, rows...)
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			c := fakedriver.NewConn()
			c.RowsAffected = 1
			db := orm.NewDB(c, postgres.Dialect{})

			n, err := run(db, []*Failing{{ID: 1, Name: "a"}, {ID: 2, Name: "b", FailAfter: true}})
			if !errors.Is(err, errHookRefused) {
				t.Fatalf("error = %v, want the hook's own error", err)
			}
			if n == 0 {
				t.Error("the count was thrown away, so the caller cannot tell what was written")
			}
		})
	}
}

// A statement covering several rows can fail like any other.
func TestInsertMany_StatementFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailOn(`INSERT INTO "users" ("username", "email", "age", "prefs", "created_at") ` +
		`VALUES ($1, $2, $3, $4, $5), ($6, $7, $8, $9, $10) RETURNING "id"`)
	db := orm.NewDB(c, postgres.Dialect{})

	err := Users.With(db).InsertMany(context.Background(),
		&User{Username: "a"}, &User{Username: "b"})
	if err == nil {
		t.Fatal("InsertMany() error = nil, want the driver failure")
	}
	if !strings.Contains(err.Error(), "inserting") {
		t.Errorf("error %q does not say what failed", err)
	}
}

// Reading a written row back can fail the same way an ordinary scan can.
func TestInsertMany_ReadBackFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"not an id"}, []any{2})
	db := orm.NewDB(c, postgres.Dialect{})

	err := Users.With(db).InsertMany(context.Background(),
		&User{Username: "a"}, &User{Username: "b"})
	if err == nil {
		t.Fatal("InsertMany() error = nil, want the read back to fail")
	}
	if !strings.Contains(err.Error(), "reading back the written row") {
		t.Errorf("error %q does not say what failed", err)
	}
}

// A row that on its own binds more parameters than the driver allows cannot
// be split any further, so one row a statement is attempted and the database
// is left to say what it makes of a row that wide.
func TestInsertMany_ARowWiderThanTheCeiling(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1})
	c.QueueRows([]any{2})
	db := returning(c, 3) // three parameters, against five columns a row

	rows := []*User{{Username: "a"}, {Username: "b"}}
	if err := Users.With(db).InsertMany(context.Background(), rows...); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}
	calls := c.QueryCalls()
	if len(calls) != 2 {
		t.Fatalf("ran %d statements, want one per row:\n%v", len(calls), calls)
	}
	for i, s := range calls {
		if strings.Contains(s, "), (") {
			t.Errorf("statement %d still covers several rows: %s", i, s)
		}
	}
}

// A key whose codec refuses its value stops the delete rather than sending
// the driver something it cannot write.
func TestDeleteMany_KeyEncodeFailure(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := BadKeys.With(db).DeleteMany(context.Background(), &badKey{Key: Prefs{Theme: "dark"}})
	if !errors.Is(err, errCannotEncode) {
		t.Errorf("DeleteMany() error = %v, want it to wrap the codec's own error", err)
	}
}

// badKey has a document column for a primary key, whose codec refuses every
// value. It is the one shape that makes a key's own encoding fail, which is
// what the delete path has to report rather than pass on.
type badKey struct {
	Key Prefs
}

type badKeyModel struct {
	orm.Table[badKey]
	Key *orm.JSONColumn[Prefs]
}

var BadKeys = orm.DefineTable[badKey]("bad_keys", func(t *orm.TableBuilder[badKey]) *badKeyModel {
	return &badKeyModel{
		Table: t.Table(),
		Key: orm.NewJSONColumn[Prefs]("key").PrimaryKey().Serialize(
			func(Prefs) ([]byte, error) { return nil, errCannotEncode },
			func([]byte) (Prefs, error) { return Prefs{}, nil },
		),
	}
})

// Failing carries hooks that refuse, on either side of the statement, so the
// two are testable apart.
type Failing struct {
	ID   int
	Name string

	// Fail and FailAfter are not columns: fields matching no column are left
	// alone by the entity mapping.
	Fail      bool
	FailAfter bool
}

type failingModel struct {
	orm.Table[Failing]
	ID   *orm.IntColumn
	Name *orm.StringColumn
}

var errHookRefused = errors.New("the hook refused")

var FailingRows = orm.DefineTable[Failing]("failing", func(t *orm.TableBuilder[Failing]) *failingModel {
	return &failingModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
	}
})

func (f *Failing) before() error {
	if f.Fail {
		return fmt.Errorf("row %d: %w", f.ID, errHookRefused)
	}
	return nil
}

func (f *Failing) after() error {
	if f.FailAfter {
		return fmt.Errorf("row %d: %w", f.ID, errHookRefused)
	}
	return nil
}

func (f *Failing) BeforeCreate(context.Context) error { return f.before() }
func (f *Failing) AfterCreate(context.Context) error  { return f.after() }
func (f *Failing) BeforeUpdate(context.Context) error { return f.before() }
func (f *Failing) AfterUpdate(context.Context) error  { return f.after() }
func (f *Failing) BeforeDelete(context.Context) error { return f.before() }
func (f *Failing) AfterDelete(context.Context) error  { return f.after() }
