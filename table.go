package orm

// Table gives a model struct its database identity. Embed it by value in
// every model struct:
//
//	type UserModel struct {
//	    Table
//	    ID *Column[int]
//	}
//
//	var User = &UserModel{Table: NewTable("users"), ID: NewColumn[int]("id")}
type Table struct {
	name string
}

// NewTable declares a model's underlying table name.
func NewTable(name string) Table {
	return Table{name: name}
}

// TableName returns the database table name.
func (t Table) TableName() string {
	return t.name
}
