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

		serverDefault, hasServerDefault := c.ServerDefaultExpr()
		if hasServerDefault && serverDefault == "" {
			return Table{}, fmt.Errorf("table %q: column %q: ServerDefault must not be empty", name, c.Name())
		}

		table.Columns = append(table.Columns, Column{
			Name:          c.Name(),
			Type:          ct,
			NotNull:       c.IsNotNull() || !c.IsNullable(),
			ServerDefault: serverDefault,
		})
		if c.IsPrimaryKey() {
			pkColumns = append(pkColumns, c.Name())
		}

		// A unique constraint already provides an index, so Index is only
		// meaningful when the column isn't already unique; folding avoids
		// a redundant separate index.
		switch {
		case c.IsUnique():
			table.Uniques = append(table.Uniques, UniqueConstraint{
				Name:    UniqueConstraintName(name, []string{c.Name()}),
				Columns: []string{c.Name()},
			})
		case c.IsIndexed():
			table.Indexes = append(table.Indexes, Index{
				Name:    IndexName(name, []string{c.Name()}),
				Columns: []string{c.Name()},
			})
		}
	}
	if len(pkColumns) > 0 {
		table.PrimaryKey = &PrimaryKey{
			Name:    PrimaryKeyConstraintName(name),
			Columns: pkColumns,
		}
		if len(pkColumns) == 1 {
			if err := validateIdentityServerDefault(name, pkColumns[0], table.Columns); err != nil {
				return Table{}, err
			}
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

	if indexer, ok := m.(orm.Indexer); ok {
		if err := mergeIndexDefs(&table, name, indexer.Indexes()); err != nil {
			return Table{}, err
		}
	}

	return table, nil
}

// mergeIndexDefs folds a model's optional, table-level Indexer definitions
// into table, auto-naming any definition left unnamed.
func mergeIndexDefs(table *Table, tableName string, defs []orm.IndexDef) error {
	for _, d := range defs {
		cols := make([]string, len(d.Columns()))
		for i, c := range d.Columns() {
			cols[i] = c.Name()
		}
		if len(cols) == 0 {
			return fmt.Errorf("table %q: index definition has no columns", tableName)
		}

		if d.IsUnique() {
			uName := d.Name()
			if uName == "" {
				uName = UniqueConstraintName(tableName, cols)
			}
			table.Uniques = append(table.Uniques, UniqueConstraint{Name: uName, Columns: cols})
			continue
		}

		iName := d.Name()
		if iName == "" {
			iName = IndexName(tableName, cols)
		}
		table.Indexes = append(table.Indexes, Index{Name: iName, Columns: cols})
	}
	return nil
}

// validateIdentityServerDefault rejects a ServerDefault on the single
// integer or bigint column driver/postgres renders as
// GENERATED ALWAYS AS IDENTITY: Postgres does not allow both that and an
// explicit DEFAULT on the same column. This mirrors RenderCreateTable's
// own identity-column rule (single-column integer or bigint primary key),
// so the conflict surfaces here, before any SQL is generated, the same
// way an invalid MaxLen already is. A composite primary key never becomes
// an identity column, so its members are never checked here.
func validateIdentityServerDefault(table, pkColumn string, columns []Column) error {
	for _, c := range columns {
		if c.Name != pkColumn {
			continue
		}
		if (c.Type.Kind == KindInteger || c.Type.Kind == KindBigInteger) && c.ServerDefault != "" {
			return fmt.Errorf(
				"table %q: column %q: ServerDefault cannot be combined with a single-column integer primary key, Postgres renders it as GENERATED ALWAYS AS IDENTITY, which does not allow an explicit DEFAULT",
				table, pkColumn)
		}
		return nil
	}
	return nil
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
