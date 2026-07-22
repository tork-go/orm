//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

// Touched has a server default, so a returned row can carry a value the
// statement never sent and the caller could not have predicted.
type rTask struct {
	ID      int
	Name    string
	State   string
	Touched int
}

type rTaskModel struct {
	orm.Table[rTask]
	ID      *orm.IntColumn
	Name    *orm.StringColumn
	State   *orm.StringColumn
	Touched *orm.IntColumn
}

var rTasks = orm.DefineTable[rTask]("r_tasks", func(t *orm.TableBuilder[rTask]) *rTaskModel {
	return &rTaskModel{
		Table:   t.Table(),
		ID:      t.Int("id").PrimaryKey(),
		Name:    t.String("name").NotNull(),
		State:   t.String("state").NotNull(),
		Touched: t.Int("touched").NotNull().ServerDefault("0"),
	}
})

// The statements are checked against a fake dialect elsewhere. This runs them
// against the database they were written for, where the returned rows have to
// be the rows the write touched and no others, and where a value the database
// computed is the point of asking for them.
func TestReturning_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS r_tasks CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(rTasks)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	ops, _ := migrate.Diff(schema.Schema{}, desired)
	ddl, err := migrate.Generate(dialect, ops)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := conn.Exec(ctx, ddl); err != nil {
		t.Fatalf("applying schema failed: %v\n%s", err, ddl)
	}

	db := orm.NewDB(conn, dialect)

	reseed := func(t *testing.T) {
		t.Helper()
		if _, err := conn.Exec(ctx, `TRUNCATE r_tasks RESTART IDENTITY`); err != nil {
			t.Fatalf("truncate failed: %v", err)
		}
		seed := []*rTask{
			{Name: "a", State: "queued"},
			{Name: "b", State: "queued"},
			{Name: "c", State: "done"},
		}
		if err := rTasks.With(db).InsertMany(ctx, seed...); err != nil {
			t.Fatalf("InsertMany failed: %v", err)
		}
	}

	names := func(rows []*rTask) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = r.Name
		}
		sort.Strings(out)
		return out
	}
	equal := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	t.Run("an update hands back exactly the rows it wrote", func(t *testing.T) {
		reseed(t)
		got, err := rTasks.With(db).Where(rTasks.State.Equals("queued")).
			UpdateAllReturning(ctx, rTasks.State.Set("running"))
		if err != nil {
			t.Fatalf("UpdateAllReturning() error = %v", err)
		}
		if want := []string{"a", "b"}; !equal(names(got), want) {
			t.Errorf("returned %v, want %v", names(got), want)
		}
		for _, r := range got {
			if r.State != "running" {
				t.Errorf("row %q came back as %q, want the state after the write", r.Name, r.State)
			}
			if r.ID == 0 {
				t.Errorf("row %q came back with no key", r.Name)
			}
		}
	})

	// The value nobody sent is the reason to ask for the rows back: reading it
	// any other way is a second statement over rows that may have moved on.
	t.Run("a column the database computed comes back", func(t *testing.T) {
		reseed(t)
		if _, err := conn.Exec(ctx, `CREATE OR REPLACE FUNCTION r_bump() RETURNS trigger AS $$
			BEGIN NEW.touched := OLD.touched + 1; RETURN NEW; END; $$ LANGUAGE plpgsql`); err != nil {
			t.Fatalf("creating the trigger function failed: %v", err)
		}
		if _, err := conn.Exec(ctx, `CREATE OR REPLACE TRIGGER r_bump_trigger
			BEFORE UPDATE ON r_tasks FOR EACH ROW EXECUTE FUNCTION r_bump()`); err != nil {
			t.Fatalf("creating the trigger failed: %v", err)
		}
		t.Cleanup(func() {
			_, _ = conn.Exec(context.Background(), `DROP TRIGGER IF EXISTS r_bump_trigger ON r_tasks`)
			_, _ = conn.Exec(context.Background(), `DROP FUNCTION IF EXISTS r_bump()`)
		})

		got, err := rTasks.With(db).Where(rTasks.Name.Equals("a")).
			UpdateAllReturning(ctx, rTasks.State.Set("running"))
		if err != nil {
			t.Fatalf("UpdateAllReturning() error = %v", err)
		}
		if len(got) != 1 || got[0].Touched != 1 {
			t.Errorf("touched = %+v, want the value the trigger wrote", got)
		}
	})

	t.Run("a delete hands back the rows it removed", func(t *testing.T) {
		reseed(t)
		got, err := rTasks.With(db).Where(rTasks.State.Equals("queued")).DeleteAllReturning(ctx)
		if err != nil {
			t.Fatalf("DeleteAllReturning() error = %v", err)
		}
		if want := []string{"a", "b"}; !equal(names(got), want) {
			t.Errorf("returned %v, want %v", names(got), want)
		}
		left, err := rTasks.With(db).Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if left != 1 {
			t.Errorf("Count() = %d, want only the done row left", left)
		}
	})

	t.Run("matching nothing returns nothing, not an error", func(t *testing.T) {
		reseed(t)
		got, err := rTasks.With(db).Where(rTasks.State.Equals("nowhere")).DeleteAllReturning(ctx)
		if err != nil {
			t.Fatalf("DeleteAllReturning() error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("returned %d rows, want none", len(got))
		}
	})

	// Claiming work is what a returning delete is for: the rows are removed
	// and handed over in one statement, so two workers cannot both take one.
	t.Run("a returning delete claims rows in one statement", func(t *testing.T) {
		reseed(t)
		claimed, err := rTasks.With(db).Where(rTasks.State.Equals("queued")).DeleteAllReturning(ctx)
		if err != nil {
			t.Fatalf("DeleteAllReturning() error = %v", err)
		}
		again, err := rTasks.With(db).Where(rTasks.State.Equals("queued")).DeleteAllReturning(ctx)
		if err != nil {
			t.Fatalf("DeleteAllReturning() error = %v", err)
		}
		if len(claimed) != 2 || len(again) != 0 {
			t.Errorf("claimed %d then %d, want 2 then 0", len(claimed), len(again))
		}
	})

	// Enough rows to be worth returning at all, so nothing about the path
	// depends on the result set being small.
	t.Run("many rows", func(t *testing.T) {
		reseed(t)
		var rows []*rTask
		for i := range 500 {
			rows = append(rows, &rTask{Name: fmt.Sprintf("bulk-%03d", i), State: "bulk"})
		}
		if err := rTasks.With(db).InsertMany(ctx, rows...); err != nil {
			t.Fatalf("InsertMany failed: %v", err)
		}
		got, err := rTasks.With(db).Where(rTasks.State.Equals("bulk")).
			UpdateAllReturning(ctx, rTasks.State.Set("swept"))
		if err != nil {
			t.Fatalf("UpdateAllReturning() error = %v", err)
		}
		if len(got) != 500 {
			t.Errorf("returned %d rows, want 500", len(got))
		}
	})
}
