//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

// A self-referencing table: the key points back at the very table holding
// it, which is the shape that cannot be joined without a second name.
type aStaff struct {
	ID        int
	Name      string
	Active    bool
	ManagerID *int
}

type aStaffModel struct {
	orm.Table[aStaff]
	ID        *orm.IntColumn
	Name      *orm.StringColumn
	Active    *orm.BoolColumn
	ManagerID *orm.NullableIntColumn
	Manager   orm.BelongsTo[aStaff]
	Reports   orm.HasMany[aStaff]
}

// Both relationships run over the one key, so neither can be inferred.
func (m *aStaffModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{
		orm.Via(&m.Manager, m.ManagerID),
		orm.Via(&m.Reports, m.ManagerID),
	}
}

var aStaffs = orm.DefineTable[aStaff]("a_staff", func(t *orm.TableBuilder[aStaff]) *aStaffModel {
	return &aStaffModel{
		Table:     t.Table(),
		ID:        t.Int("id").PrimaryKey(),
		Name:      t.String("name").NotNull().MaxLen(40),
		Active:    t.Bool("active").NotNull(),
		ManagerID: t.NullableInt("manager_id").ReferencesTable("a_staff", "id"),
	}
})

// A table related to nothing, for the join written on conditions rather
// than on a declared key.
type aBadge struct {
	ID      int
	StaffID int
	Label   string
}

type aBadgeModel struct {
	orm.Table[aBadge]
	ID      *orm.IntColumn
	StaffID *orm.IntColumn
	Label   *orm.StringColumn
}

var aBadges = orm.DefineTable[aBadge]("a_badges", func(t *orm.TableBuilder[aBadge]) *aBadgeModel {
	return &aBadgeModel{
		Table:   t.Table(),
		ID:      t.Int("id").PrimaryKey(),
		StaffID: t.Int("staff_id").NotNull(),
		Label:   t.String("label").NotNull().MaxLen(20),
	}
})

// The compile tests assert the statement; this asserts the answer. A self
// join is the case where the two differ most: the unaliased statement is
// not merely differently shaped, it is one Postgres refuses outright.
func TestAliases_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS a_staff, a_badges CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(aStaffs, aBadges)
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

	// Ada manages Ben and Cal; Cal manages Dee. Ada reports to nobody, and
	// Ben is inactive, so a condition on the manager can be told apart from
	// one on the employee.
	if _, err := conn.Exec(ctx, `
		INSERT INTO a_staff (id, name, active, manager_id) OVERRIDING SYSTEM VALUE VALUES
			(1, 'ada', true,  NULL),
			(2, 'ben', false, 1),
			(3, 'cal', true,  1),
			(4, 'dee', true,  3)`); err != nil {
		t.Fatalf("seeding staff failed: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO a_badges (id, staff_id, label) OVERRIDING SYSTEM VALUE VALUES
			(1, 1, 'founder'), (2, 3, 'mentor')`); err != nil {
		t.Fatalf("seeding badges failed: %v", err)
	}

	db := orm.NewDB(conn, dialect)
	mgr := orm.Alias(aStaffs, "mgr")

	// The motivating case: everyone whose manager is Ada.
	t.Run("self join filters on the manager", func(t *testing.T) {
		got, err := aStaffs.With(db).
			JoinAs(aStaffs.Manager, mgr).
			Where(mgr.Name.Equals("ada")).
			OrderBy(aStaffs.ID.Asc()).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if names := staffNames(got); len(names) != 2 || names[0] != "ben" || names[1] != "cal" {
			t.Errorf("All() = %v, want [ben cal]", names)
		}
	})

	// Reading both sides at once, which is what a self join is usually for.
	t.Run("projection reads both sides", func(t *testing.T) {
		type pair struct {
			Staff   string
			Manager string
		}
		got, err := orm.SelectAs[pair](
			aStaffs.With(db).JoinAs(aStaffs.Manager, mgr),
			aStaffs.Name, mgr.Name,
		).OrderBy(aStaffs.Name.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("SelectAs.All() error = %v", err)
		}
		want := []pair{{"ben", "ada"}, {"cal", "ada"}, {"dee", "cal"}}
		if len(got) != len(want) {
			t.Fatalf("SelectAs.All() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("row %d = %v, want %v", i, got[i], want[i])
			}
		}
	})

	// A left join keeps the one person who reports to nobody: no manager
	// row matched, so every column read through the alias comes back NULL.
	t.Run("left self join keeps the unmatched row", func(t *testing.T) {
		got, err := aStaffs.With(db).
			LeftJoinAs(aStaffs.Manager, mgr).
			Where(mgr.ManagerID.IsNull(), aStaffs.ManagerID.IsNull()).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if names := staffNames(got); len(names) != 1 || names[0] != "ada" {
			t.Errorf("All() = %v, want [ada]", names)
		}
	})

	// The condition sits on the join rather than after it, so a person
	// whose manager is inactive still comes back — with no manager matched.
	t.Run("left join on keeps rows the condition excluded", func(t *testing.T) {
		type pair struct {
			Staff   string
			Manager *string
		}
		got, err := orm.SelectAs[pair](
			aStaffs.With(db).LeftJoinOnAs(aStaffs.Manager, mgr, mgr.Active.Equals(true)),
			aStaffs.Name, mgr.Name,
		).OrderBy(aStaffs.Name.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("SelectAs.All() error = %v", err)
		}
		if len(got) != 4 {
			t.Fatalf("SelectAs.All() returned %d rows, want 4", len(got))
		}
		// ada reports to nobody, so nothing matches her however the
		// condition reads; the other three all report to an active manager.
		// The point is that ada still comes back at all, which a WHERE on
		// the same condition would have dropped.
		want := map[string]string{"ben": "ada", "cal": "ada", "dee": "cal"}
		for _, row := range got {
			if row.Staff == "ada" {
				if row.Manager != nil {
					t.Errorf("ada's manager = %v, want none matched", *row.Manager)
				}
				continue
			}
			if row.Manager == nil || *row.Manager != want[row.Staff] {
				t.Errorf("%s's manager = %v, want %s", row.Staff, row.Manager, want[row.Staff])
			}
		}
	})

	// Two names for the one table in a single statement: who manages
	// somebody who in turn manages somebody.
	t.Run("two aliases in one statement", func(t *testing.T) {
		report := orm.Alias(aStaffs, "report")
		grand := orm.Alias(aStaffs, "grand")
		got, err := aStaffs.With(db).
			JoinAs(aStaffs.Reports, report).
			JoinTo(grand, grand.ManagerID.Value().Equals(report.ID)).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if names := staffNames(got); len(names) != 1 || names[0] != "ada" {
			t.Errorf("All() = %v, want [ada]", names)
		}
	})

	// Two tables with no relationship declared between them.
	t.Run("join to an unrelated table", func(t *testing.T) {
		got, err := aStaffs.With(db).
			JoinTo(aBadges, aBadges.StaffID.Value().Equals(aStaffs.ID)).
			Where(aBadges.Label.Equals("mentor")).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if names := staffNames(got); len(names) != 1 || names[0] != "cal" {
			t.Errorf("All() = %v, want [cal]", names)
		}
	})

	// Reading through an alias reads the stored table's own rows.
	t.Run("read through an alias", func(t *testing.T) {
		got, err := mgr.With(db).Where(mgr.Active.Equals(false)).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if names := staffNames(got); len(names) != 1 || names[0] != "ben" {
			t.Errorf("All() = %v, want [ben]", names)
		}
	})

	// The guard is worth having only if the statement it refuses is one
	// Postgres refuses too. This is that check, from the other side.
	t.Run("the unaliased self join is what Postgres rejects", func(t *testing.T) {
		if _, err := aStaffs.With(db).Join(aStaffs.Manager).All(ctx); err == nil {
			t.Fatal("All() error = nil, want the unaliased self join refused")
		}
		const sql = `SELECT "a_staff"."id" FROM "a_staff" ` +
			`JOIN "a_staff" ON "a_staff"."id" = "a_staff"."manager_id"`
		if _, err := conn.Query(ctx, sql); err == nil {
			t.Error("Postgres accepted the unaliased self join; the guard would be wrong")
		}
	})
}

func staffNames(rows []*aStaff) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}
