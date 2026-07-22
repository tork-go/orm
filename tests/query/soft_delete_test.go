package query_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// SoftDeletable carries no default scope of its own, so its tests isolate
// the soft-delete rewrite from the Scoper half scope_test.go's ScopedPosts
// fixture exercises together with it.
type SoftDeletable struct {
	ID        int
	Name      string
	DeletedAt *time.Time

	fired []string
}

type SoftDeletableModel struct {
	orm.Table[SoftDeletable]
	ID        *orm.IntColumn
	Name      *orm.StringColumn
	DeletedAt *orm.NullableTimeColumn
}

var SoftDeletables = orm.DefineTable[SoftDeletable]("soft_deletables",
	func(t *orm.TableBuilder[SoftDeletable]) *SoftDeletableModel {
		return &SoftDeletableModel{
			Table:     t.Table(),
			ID:        t.Int("id").PrimaryKey(),
			Name:      t.String("name").NotNull(),
			DeletedAt: t.NullableTime("deleted_at").SoftDelete(),
		}
	})

func (s *SoftDeletable) BeforeDelete(context.Context) error {
	s.fired = append(s.fired, "BeforeDelete")
	return nil
}
func (s *SoftDeletable) AfterDelete(context.Context) error {
	s.fired = append(s.fired, "AfterDelete")
	return nil
}

// SoftDeletableNoPK has a soft-delete column but no primary key, so a
// single-row Delete has no key to identify its row by. It exists to reach
// that error from inside the soft-delete branch of writer.delete, which a
// table with a primary key never can.
type SoftDeletableNoPK struct {
	Name      string
	DeletedAt *time.Time
}

type SoftDeletableNoPKModel struct {
	orm.Table[SoftDeletableNoPK]
	Name      *orm.StringColumn
	DeletedAt *orm.NullableTimeColumn
}

var SoftDeletablesNoPK = orm.DefineTable[SoftDeletableNoPK]("soft_deletables_no_pk",
	func(t *orm.TableBuilder[SoftDeletableNoPK]) *SoftDeletableNoPKModel {
		return &SoftDeletableNoPKModel{
			Table:     t.Table(),
			Name:      t.String("name").NotNull(),
			DeletedAt: t.NullableTime("deleted_at").SoftDelete(),
		}
	})

func TestSoftDelete_DeleteNoPrimaryKey(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	err := SoftDeletablesNoPK.With(db).Delete(context.Background(), &SoftDeletableNoPK{Name: "x"})
	if err == nil {
		t.Fatal("Delete() error = nil, want a primary key error")
	}
	if !strings.Contains(err.Error(), "primary key") {
		t.Errorf("error %q does not mention a primary key", err)
	}
}

// The UPDATE a soft delete issues fails exactly like the DELETE it replaces
// would: the driver's own error, wrapped and named.
func TestSoftDelete_DeleteExecFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailOn(`UPDATE "soft_deletables" SET "deleted_at" = $1 WHERE "id" = $2`)
	db := orm.NewDB(c, postgres.Dialect{})

	err := SoftDeletables.With(db).Delete(context.Background(), &SoftDeletable{ID: 3})
	if err == nil {
		t.Fatal("Delete() error = nil, want the driver's failure")
	}
	if !strings.Contains(err.Error(), "deleting") {
		t.Errorf("error %q does not say what it was doing", err)
	}
}

// Delete stamps the soft-delete column instead of removing the row, and
// still fires BeforeDelete/AfterDelete exactly as an ordinary Delete does:
// only the SQL it issues changes.
func TestSoftDelete_DeleteBecomesUpdate(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	row := &SoftDeletable{ID: 3}
	if err := SoftDeletables.With(db).Delete(context.Background(), row); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	want := `UPDATE "soft_deletables" SET "deleted_at" = $1 WHERE "id" = $2`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("Delete ran %s\nwant       %s", got, want)
	}
	if args := c.ExecArgs(0); len(args) != 2 || args[1] != 3 {
		t.Errorf("Delete bound %v, want [<now> 3]", args)
	}
	if strings.Join(row.fired, ",") != "BeforeDelete,AfterDelete" {
		t.Errorf("fired = %v, want [BeforeDelete AfterDelete]", row.fired)
	}
}

func TestSoftDelete_DeleteIfBecomesUpdate(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	row := &SoftDeletable{ID: 3}
	if err := SoftDeletables.With(db).DeleteIf(context.Background(), row, SoftDeletables.Name.Equals("x")); err != nil {
		t.Fatalf("DeleteIf() error = %v", err)
	}
	want := `UPDATE "soft_deletables" SET "deleted_at" = $1 WHERE ("id" = $2 AND "name" = $3)`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("DeleteIf ran %s\nwant        %s", got, want)
	}
}

// ForceDelete always removes the row, even when the table has a soft-delete
// column, and fires the same hooks Delete does.
func TestSoftDelete_ForceDelete_IsHardDelete(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	row := &SoftDeletable{ID: 3}
	if err := SoftDeletables.With(db).ForceDelete(context.Background(), row); err != nil {
		t.Fatalf("ForceDelete() error = %v", err)
	}
	want := `DELETE FROM "soft_deletables" WHERE "id" = $1`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("ForceDelete ran %s\nwant            %s", got, want)
	}
	if strings.Join(row.fired, ",") != "BeforeDelete,AfterDelete" {
		t.Errorf("fired = %v, want [BeforeDelete AfterDelete]", row.fired)
	}
}

func TestSoftDelete_ForceDelete_NoSuchRow(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	err := SoftDeletables.With(db).ForceDelete(context.Background(), &SoftDeletable{ID: 3})
	if err == nil {
		t.Fatal("ForceDelete() error = nil, want ErrNoRows")
	}
}

// DeleteAll updates rather than deletes when the table has a soft-delete
// column, carrying the implicit "not yet deleted" scope in its WHERE the
// same way any other statement over this table does, so it never touches a
// row already marked deleted.
func TestSoftDelete_DeleteAllBecomesUpdate(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 2
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := SoftDeletables.With(db).Where(SoftDeletables.Name.Equals("x")).DeleteAll(context.Background())
	if err != nil {
		t.Fatalf("DeleteAll() error = %v", err)
	}
	if n != 2 {
		t.Errorf("DeleteAll() = %d, want 2", n)
	}
	want := `UPDATE "soft_deletables" SET "deleted_at" = $1 WHERE ("name" = $2 AND "deleted_at" IS NULL)`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("DeleteAll ran %s\nwant         %s", got, want)
	}
}

func TestSoftDelete_DeleteAllReturningBecomesUpdate(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "x", nil})
	db := orm.NewDB(c, postgres.Dialect{})

	rows, err := SoftDeletables.With(db).Where(SoftDeletables.Name.Equals("x")).DeleteAllReturning(context.Background())
	if err != nil {
		t.Fatalf("DeleteAllReturning() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("DeleteAllReturning() returned %d rows, want 1", len(rows))
	}
	want := `UPDATE "soft_deletables" SET "deleted_at" = $1 ` +
		`WHERE ("name" = $2 AND "deleted_at" IS NULL) RETURNING "id", "name", "deleted_at"`
	if got := c.QueryCalls()[0]; got != want {
		t.Errorf("DeleteAllReturning ran %s\nwant                   %s", got, want)
	}
}

// ForceDeleteAll and ForceDeleteAllReturning always issue a physical
// DELETE rather than an UPDATE, but that is the only thing force changes:
// the implicit scope still hides an already soft-deleted row, the same as
// it would from a read. A row already marked deleted is not touched by
// ForceDeleteAll alone; Unscoped is what reaches it too, see
// TestSoftDelete_UnscopedForceDeleteAll_PurgesAlreadyDeletedRows.
func TestSoftDelete_ForceDeleteAll_IsHardDelete(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 2
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := SoftDeletables.With(db).Where(SoftDeletables.Name.Equals("x")).ForceDeleteAll(context.Background())
	if err != nil {
		t.Fatalf("ForceDeleteAll() error = %v", err)
	}
	if n != 2 {
		t.Errorf("ForceDeleteAll() = %d, want 2", n)
	}
	want := `DELETE FROM "soft_deletables" WHERE ("name" = $1 AND "deleted_at" IS NULL)`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("ForceDeleteAll ran %s\nwant              %s", got, want)
	}
}

func TestSoftDelete_ForceDeleteAllReturning_IsHardDelete(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "x", nil})
	db := orm.NewDB(c, postgres.Dialect{})

	rows, err := SoftDeletables.With(db).Where(SoftDeletables.Name.Equals("x")).
		ForceDeleteAllReturning(context.Background())
	if err != nil {
		t.Fatalf("ForceDeleteAllReturning() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ForceDeleteAllReturning() returned %d rows, want 1", len(rows))
	}
	want := `DELETE FROM "soft_deletables" WHERE ("name" = $1 AND "deleted_at" IS NULL) ` +
		`RETURNING "id", "name", "deleted_at"`
	if got := c.QueryCalls()[0]; got != want {
		t.Errorf("ForceDeleteAllReturning ran %s\nwant                       %s", got, want)
	}
}

// Chaining Unscoped in front of ForceDeleteAll is what actually purges
// every row, including ones a previous Delete already marked: force alone
// only changes UPDATE to DELETE, and Unscoped alone only changes which rows
// a read or a soft delete can see, so reaching a soft-deleted row with a
// real DELETE needs both.
func TestSoftDelete_UnscopedForceDeleteAll_PurgesAlreadyDeletedRows(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 3
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := SoftDeletables.With(db).Unscoped().ForceDeleteAll(context.Background())
	if err != nil {
		t.Fatalf("ForceDeleteAll() error = %v", err)
	}
	if n != 3 {
		t.Errorf("ForceDeleteAll() = %d, want 3", n)
	}
	if got := c.ExecCalls()[0]; got != `DELETE FROM "soft_deletables"` {
		t.Errorf("ForceDeleteAll ran %s, want no WHERE at all", got)
	}
}

// Query's unfiltered forwarders reach ForceDeleteAll/ForceDeleteAllReturning
// the same way they reach every other set operation. The implicit scope
// still applies: not calling Where is the caller saying "every row I can
// see", not "every row, full stop".
func TestSoftDelete_ForceDeleteAllFromQuery(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 5
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := SoftDeletables.With(db).ForceDeleteAll(context.Background())
	if err != nil {
		t.Fatalf("ForceDeleteAll() error = %v", err)
	}
	if n != 5 {
		t.Errorf("ForceDeleteAll() = %d, want 5", n)
	}
	want := `DELETE FROM "soft_deletables" WHERE "deleted_at" IS NULL`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("ForceDeleteAll ran %s\nwant              %s", got, want)
	}
}

func TestSoftDelete_ForceDeleteAllReturningFromQuery(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "x", nil})
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := SoftDeletables.With(db).ForceDeleteAllReturning(context.Background()); err != nil {
		t.Fatalf("ForceDeleteAllReturning() error = %v", err)
	}
	want := `DELETE FROM "soft_deletables" WHERE "deleted_at" IS NULL RETURNING "id", "name", "deleted_at"`
	if got := c.QueryCalls()[0]; got != want {
		t.Errorf("ForceDeleteAllReturning ran %s\nwant                       %s", got, want)
	}
}

// DeleteMany, the batch write over rows already in hand, is as consistent
// with Delete as DeleteAll is: soft delete updates rather than removes,
// unless ForceDeleteMany asks for a real DELETE.
func TestSoftDelete_DeleteManyBecomesUpdate(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 2
	db := orm.NewDB(c, postgres.Dialect{})

	rows := []*SoftDeletable{{ID: 1}, {ID: 2}}
	n, err := SoftDeletables.With(db).DeleteMany(context.Background(), rows...)
	if err != nil {
		t.Fatalf("DeleteMany() error = %v", err)
	}
	if n != 2 {
		t.Errorf("DeleteMany() = %d, want 2", n)
	}
	want := `UPDATE "soft_deletables" SET "deleted_at" = $1 WHERE "id" IN ($2, $3)`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("DeleteMany ran %s\nwant          %s", got, want)
	}
	for _, row := range rows {
		if strings.Join(row.fired, ",") != "BeforeDelete,AfterDelete" {
			t.Errorf("row %d fired = %v, want [BeforeDelete AfterDelete]", row.ID, row.fired)
		}
	}
}

// The UPDATE a soft-deleting DeleteMany issues fails exactly like the
// DELETE it replaces would.
func TestSoftDelete_DeleteManyExecFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailOn(`UPDATE "soft_deletables" SET "deleted_at" = $1 WHERE "id" IN ($2)`)
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := SoftDeletables.With(db).DeleteMany(context.Background(), &SoftDeletable{ID: 1})
	if err == nil {
		t.Fatal("DeleteMany() error = nil, want the driver's failure")
	}
	if !strings.Contains(err.Error(), "deleting") {
		t.Errorf("error %q does not say what failed", err)
	}
}

func TestSoftDelete_ForceDeleteMany_IsHardDelete(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 2
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := SoftDeletables.With(db).ForceDeleteMany(context.Background(),
		&SoftDeletable{ID: 1}, &SoftDeletable{ID: 2})
	if err != nil {
		t.Fatalf("ForceDeleteMany() error = %v", err)
	}
	if n != 2 {
		t.Errorf("ForceDeleteMany() = %d, want 2", n)
	}
	want := `DELETE FROM "soft_deletables" WHERE "id" IN ($1, $2)`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("ForceDeleteMany ran %s\nwant               %s", got, want)
	}
}

// The stamped value is an ordinary Go time.Time bound like any other
// assignment, not a SQL literal: the codebase binds values through
// argBuilder everywhere else, and soft delete is no exception.
func TestSoftDelete_TimeIsBoundAsGoValue(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	before := time.Now()
	if err := SoftDeletables.With(db).Delete(context.Background(), &SoftDeletable{ID: 1}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	after := time.Now()

	args := c.ExecArgs(0)
	if len(args) != 2 {
		t.Fatalf("bound %d args, want 2", len(args))
	}
	stamped, ok := args[0].(time.Time)
	if !ok {
		t.Fatalf("first bound arg is %T, want time.Time", args[0])
	}
	if stamped.Before(before) || stamped.After(after) {
		t.Errorf("stamped %v, want between %v and %v", stamped, before, after)
	}
}

// A read sees the same rows either way: the implicit scope defaultScope
// adds for a soft-delete column excludes a deleted row exactly as a
// Scoper-declared one would.
func TestSoftDelete_ImplicitScopeExcludesDeletedRows(t *testing.T) {
	sql, args, err := SoftDeletables.With(pg()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "name", "deleted_at" FROM "soft_deletables" WHERE "deleted_at" IS NULL`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none", args)
	}
}

func TestSoftDelete_UnscopedIncludesDeletedRows(t *testing.T) {
	sql, _, err := SoftDeletables.With(pg()).Unscoped().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT "id", "name", "deleted_at" FROM "soft_deletables"`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// A table may declare at most one soft-delete column: two would leave
// Delete and DeleteAll with no way to choose which to stamp.
func TestDefineTable_TwoSoftDeleteColumns_Panics(t *testing.T) {
	type twoMarkers struct {
		ID        int
		DeletedAt *time.Time
		RemovedAt *time.Time
	}
	type twoMarkersModel struct {
		orm.Table[twoMarkers]
		ID        *orm.IntColumn
		DeletedAt *orm.NullableTimeColumn
		RemovedAt *orm.NullableTimeColumn
	}

	var got string
	func() {
		defer func() {
			if r := recover(); r != nil {
				got, _ = r.(string)
			}
		}()
		orm.DefineTable[twoMarkers]("two_markers", func(b *orm.TableBuilder[twoMarkers]) *twoMarkersModel {
			return &twoMarkersModel{
				Table:     b.Table(),
				ID:        b.Int("id").PrimaryKey(),
				DeletedAt: b.NullableTime("deleted_at").SoftDelete(),
				RemovedAt: b.NullableTime("removed_at").SoftDelete(),
			}
		})
		t.Fatal("DefineTable did not panic, want a panic naming both soft-delete columns")
	}()

	for _, want := range []string{`table "two_markers"`, `"deleted_at"`, `"removed_at"`, "only one is allowed"} {
		if !strings.Contains(got, want) {
			t.Errorf("panic message %q does not mention %q", got, want)
		}
	}
}
