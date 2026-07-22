package query_test

import "github.com/tork-go/orm"

// A self-referencing table, and a table related to nothing, which are the
// two shapes aliasing exists for: one statement reaching the same table
// twice, and two tables with no relationship declared between them.

// Employee reports to another employee, so the foreign key points back at
// the very table that holds it.
type Employee struct {
	ID        int
	Name      string
	Active    bool
	ManagerID *int

	Manager *Employee  // BelongsTo, onto this same table
	Reports []Employee // HasMany, from the other end of the same key
}

type EmployeeModel struct {
	orm.Table[Employee]
	ID        *orm.IntColumn
	Name      *orm.StringColumn
	Active    *orm.BoolColumn
	ManagerID *orm.NullableIntColumn
	Manager   orm.BelongsTo[Employee]
	Reports   orm.HasMany[Employee]
}

// Both relationships run over the one key, so neither can be inferred:
// soleForeignKeyInto finds manager_id for each and has no way to tell which
// field means which direction. Naming the key settles it.
func (m *EmployeeModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{
		orm.Via(&m.Manager, m.ManagerID),
		orm.Via(&m.Reports, m.ManagerID),
	}
}

var Employees = orm.DefineTable[Employee]("employees",
	func(t *orm.TableBuilder[Employee]) *EmployeeModel {
		return &EmployeeModel{
			Table:     t.Table(),
			ID:        t.Int("id").PrimaryKey(),
			Name:      t.String("name").NotNull().MaxLen(40),
			Active:    t.Bool("active").NotNull(),
			ManagerID: t.NullableInt("manager_id").ReferencesTable("employees", "id"),
		}
	})

// Login records an attempt against a user, without declaring a foreign key
// or a relationship — the case JoinTo is for.
type Login struct {
	ID     int
	UserID int
	Failed bool
}

type LoginModel struct {
	orm.Table[Login]
	ID     *orm.IntColumn
	UserID *orm.IntColumn
	Failed *orm.BoolColumn
}

var Logins = orm.DefineTable[Login]("logins", func(t *orm.TableBuilder[Login]) *LoginModel {
	return &LoginModel{
		Table:  t.Table(),
		ID:     t.Int("id").PrimaryKey(),
		UserID: t.Int("user_id").NotNull(),
		Failed: t.Bool("failed").NotNull(),
	}
})

// employeeCols is the SELECT list every expected statement over employees
// starts with, qualified as a joined statement writes it.
const employeeCols = `"employees"."id", "employees"."name", ` +
	`"employees"."active", "employees"."manager_id"`
