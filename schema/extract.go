package schema

import (
	"fmt"

	"github.com/tork-go/orm"
)

// ExtractSchema builds the desired Schema from a set of Go models.
func ExtractSchema(models ...orm.Model) (Schema, error) {
	var s Schema
	for _, m := range models {
		table, err := extractTable(m)
		if err != nil {
			return Schema{}, err
		}
		s.Tables = append(s.Tables, table)
	}
	return s, nil
}

func extractTable(m orm.Model) (Table, error) {
	name := m.TableName()
	table := Table{Name: name}

	var pkColumns []string
	for _, c := range orm.Columns(m) {
		ct, err := columnType(c)
		if err != nil {
			return Table{}, fmt.Errorf("table %q: %w", name, err)
		}
		table.Columns = append(table.Columns, Column{
			Name:    c.Name(),
			Type:    ct,
			NotNull: c.IsNotNull() || !c.IsNullable(),
		})
		if c.IsPrimaryKey() {
			pkColumns = append(pkColumns, c.Name())
		}
		if c.IsUnique() {
			table.Uniques = append(table.Uniques, UniqueConstraint{
				Name:    UniqueConstraintName(name, []string{c.Name()}),
				Columns: []string{c.Name()},
			})
		}
	}
	if len(pkColumns) > 0 {
		table.PrimaryKey = &PrimaryKey{
			Name:    PrimaryKeyConstraintName(name),
			Columns: pkColumns,
		}
	}

	for _, fk := range orm.ForeignKeys(m) {
		table.ForeignKeys = append(table.ForeignKeys, ForeignKey{
			Name:              ForeignKeyConstraintName(name, []string{fk.Name()}),
			Columns:           []string{fk.Name()},
			ReferencedTable:   fk.ReferencedTable(),
			ReferencedColumns: []string{fk.ReferencedColumn()},
		})
	}

	return table, nil
}

// columnType resolves a column's ColumnType, and validates that MaxLen was
// used correctly: only on string columns (Kind == KindText, the default
// Go-string mapping), and only with a positive length.
func columnType(c orm.ColumnMeta) (ColumnType, error) {
	kind, err := KindForGoType(c.GoType())
	if err != nil {
		return ColumnType{}, fmt.Errorf("column %q: %w", c.Name(), err)
	}

	n, ok := c.MaxLength()
	if !ok {
		return ColumnType{Kind: kind}, nil
	}
	if kind != KindText {
		return ColumnType{}, fmt.Errorf("column %q: MaxLen is only valid on string columns", c.Name())
	}
	if n <= 0 {
		return ColumnType{}, fmt.Errorf("column %q: MaxLen must be positive, got %d", c.Name(), n)
	}
	return ColumnType{Kind: KindVarchar, Length: n}, nil
}
