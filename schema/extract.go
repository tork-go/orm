package schema

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/tork-go/orm"
)

// ExtractSchema builds the desired Schema from a set of Go models.
func ExtractSchema(models ...orm.Model) (Schema, error) {
	var s Schema
	seen := make(map[string]EnumType)
	for _, m := range models {
		table, enums, err := extractTable(m)
		if err != nil {
			return Schema{}, err
		}
		for _, e := range enums {
			existing, ok := seen[e.Name]
			if !ok {
				seen[e.Name] = e
				s.EnumTypes = append(s.EnumTypes, e)
				continue
			}
			if !slices.Equal(existing.Values, e.Values) {
				return Schema{}, fmt.Errorf(
					"enum type %q: declared with different values in different columns (%v vs %v)",
					e.Name, existing.Values, e.Values)
			}
		}
		s.Tables = append(s.Tables, table)
	}
	return s, nil
}

func extractTable(m orm.Model) (Table, []EnumType, error) {
	name := m.TableName()
	table := Table{Name: name}
	var enums []EnumType

	var pkColumns []string
	for _, c := range orm.Columns(m) {
		ct, err := columnType(c)
		if err != nil {
			return Table{}, nil, fmt.Errorf("table %q: %w", name, err)
		}
		if ct.Kind == KindEnum {
			_, values, _ := c.EnumSpec()
			enums = append(enums, EnumType{Name: ct.TypeName, Values: values})
		}

		serverDefault, hasServerDefault := c.ServerDefaultExpr()
		if hasServerDefault && serverDefault == "" {
			return Table{}, nil, fmt.Errorf("table %q: column %q: ServerDefault must not be empty", name, c.Name())
		}

		table.Columns = append(table.Columns, Column{
			Name:          c.Name(),
			Type:          ct,
			NotNull:       c.HasNotNull() || !c.IsNullable(),
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
				return Table{}, nil, err
			}
		}
	}

	for _, fk := range orm.ForeignKeys(m) {
		table.ForeignKeys = append(table.ForeignKeys, ForeignKey{
			Name:              ForeignKeyConstraintName(name, []string{fk.Name()}),
			Columns:           []string{fk.Name()},
			ReferencedTable:   fk.ReferencedTable(),
			ReferencedColumns: []string{fk.ReferencedColumn()},
			OnDelete:          convertAction(fk.OnDeleteAction()),
			OnUpdate:          convertAction(fk.OnUpdateAction()),
		})
	}

	if indexer, ok := m.(orm.Indexer); ok {
		if err := mergeIndexDefs(&table, name, indexer.Indexes()); err != nil {
			return Table{}, nil, err
		}
	}

	if checker, ok := m.(orm.Checker); ok {
		if err := mergeCheckDefs(&table, name, checker.Checks()); err != nil {
			return Table{}, nil, err
		}
	}

	return table, enums, nil
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

// mergeCheckDefs folds a model's optional, table-level Checker definitions
// into table, auto-naming any definition left unnamed with a positional
// name (see CheckConstraintName).
func mergeCheckDefs(table *Table, tableName string, defs []orm.CheckDef) error {
	unnamed := 0
	for _, d := range defs {
		if d.Expression() == "" {
			return fmt.Errorf("table %q: check definition has no expression", tableName)
		}
		name := d.Name()
		if name == "" {
			unnamed++
			name = CheckConstraintName(tableName, unnamed)
		}
		table.Checks = append(table.Checks, Check{Name: name, Expression: d.Expression()})
	}
	return nil
}

// convertAction maps orm.ForeignKeyAction to schema's own mirror type.
// Kept distinct from orm.ForeignKeyAction so driver/postgres, which builds
// schema.ForeignKey values from introspection alone, never needs to
// import orm.
func convertAction(a orm.ForeignKeyAction) ForeignKeyAction {
	switch a {
	case orm.ActionCascade:
		return ActionCascade
	case orm.ActionSetNull:
		return ActionSetNull
	case orm.ActionSetDefault:
		return ActionSetDefault
	case orm.ActionRestrict:
		return ActionRestrict
	default:
		return ActionNoAction
	}
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

// columnType resolves a column's ColumnType, dispatching to the special
// cases (JSON/JSONB, Enum, Array) before falling back to a scalar type
// driven by KindForGoType. JSON/JSONB and Enum are checked first, before
// KindForGoType runs at all: JSON/JSONB accepts any T since encoding/json
// can marshal almost anything, and Enum's T is typically a named string
// type (e.g. type OrderStatus string) that KindForGoType would otherwise
// reject, since it isn't the exact string type.
func columnType(c orm.ColumnMeta) (ColumnType, error) {
	if c.IsJSON() || c.IsJSONB() {
		return jsonColumnType(c)
	}
	if _, _, ok := c.EnumSpec(); ok {
		return enumColumnType(c)
	}

	kind, err := KindForGoType(c.GoType())
	if err != nil {
		return ColumnType{}, fmt.Errorf("column %q: %w", c.Name(), err)
	}
	if kind == KindArray {
		return arrayColumnType(c)
	}
	return scalarColumnType(c, kind)
}

// jsonColumnType resolves a column marked JSON or JSONB (via JSON, JSONB,
// or Serialize, which implies JSONB when called alone). MaxLen, Numeric,
// and Enum are all meaningless on a JSON/JSONB column, and rejected here.
func jsonColumnType(c orm.ColumnMeta) (ColumnType, error) {
	kind := KindJSONB
	if c.IsJSON() {
		kind = KindJSON
	}
	if n, ok := c.MaxLength(); ok && n != 0 {
		return ColumnType{}, fmt.Errorf("column %q: MaxLen is not valid on a JSON/JSONB column", c.Name())
	}
	if _, _, ok := c.NumericPrecisionScale(); ok {
		return ColumnType{}, fmt.Errorf("column %q: Numeric is not valid on a JSON/JSONB column", c.Name())
	}
	if _, _, ok := c.EnumSpec(); ok {
		return ColumnType{}, fmt.Errorf("column %q: Enum cannot be combined with JSON/JSONB", c.Name())
	}
	return ColumnType{Kind: kind}, nil
}

// enumColumnType resolves a column with Enum called. T must resolve to a
// string kind once pointer nullability is unwrapped.
func enumColumnType(c orm.ColumnMeta) (ColumnType, error) {
	typeName, values, _ := c.EnumSpec()

	t := c.GoType()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.String {
		return ColumnType{}, fmt.Errorf("column %q: Enum is only valid on a string-kind column", c.Name())
	}
	if typeName == "" {
		return ColumnType{}, fmt.Errorf("column %q: Enum type name must not be empty", c.Name())
	}
	if len(values) == 0 {
		return ColumnType{}, fmt.Errorf("column %q: Enum must have at least one value", c.Name())
	}
	seen := make(map[string]bool, len(values))
	for _, v := range values {
		if seen[v] {
			return ColumnType{}, fmt.Errorf("column %q: Enum values must be unique, duplicate %q", c.Name(), v)
		}
		seen[v] = true
	}
	if n, ok := c.MaxLength(); ok && n != 0 {
		return ColumnType{}, fmt.Errorf("column %q: MaxLen is not valid on an Enum column", c.Name())
	}
	if _, _, ok := c.NumericPrecisionScale(); ok {
		return ColumnType{}, fmt.Errorf("column %q: Numeric is not valid on an Enum column", c.Name())
	}
	return ColumnType{Kind: KindEnum, TypeName: typeName}, nil
}

// arrayColumnType resolves a Column[[]T] column. MaxLen/Numeric, if set,
// apply to the element type rather than the array itself, since a Go
// array/slice type carries no bounded-length or precision information of
// its own.
func arrayColumnType(c orm.ColumnMeta) (ColumnType, error) {
	t := c.GoType()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	elemType := t.Elem()
	if elemType.Kind() == reflect.Slice {
		return ColumnType{}, fmt.Errorf("column %q: multi-dimensional arrays are not supported", c.Name())
	}

	elemKind, err := KindForGoType(elemType)
	if err != nil {
		return ColumnType{}, fmt.Errorf("column %q: array element: %w", c.Name(), err)
	}
	if elemKind == KindArray {
		return ColumnType{}, fmt.Errorf("column %q: multi-dimensional arrays are not supported", c.Name())
	}

	elem := ColumnType{Kind: elemKind}
	if n, ok := c.MaxLength(); ok {
		if elemKind != KindText {
			return ColumnType{}, fmt.Errorf("column %q: MaxLen is only valid on a string column or a string-array column", c.Name())
		}
		if n <= 0 {
			return ColumnType{}, fmt.Errorf("column %q: MaxLen must be positive, got %d", c.Name(), n)
		}
		elem = ColumnType{Kind: KindVarchar, Length: n}
	}
	if p, s, ok := c.NumericPrecisionScale(); ok {
		if elemKind != KindNumeric {
			return ColumnType{}, fmt.Errorf("column %q: Numeric is only valid on a numeric column or a numeric-array column", c.Name())
		}
		if err := validateNumericPrecisionScale(c.Name(), p, s); err != nil {
			return ColumnType{}, err
		}
		elem = ColumnType{Kind: KindNumeric, Precision: p, Scale: s}
	}

	return ColumnType{Kind: KindArray, Elem: &elem}, nil
}

// scalarColumnType applies MaxLen/Numeric validation for a scalar
// (non-array, non-JSON, non-Enum) column of kind, and validates that
// MaxLen was used correctly: only on string columns (Kind == KindText,
// the default Go-string mapping), and only with a positive length.
func scalarColumnType(c orm.ColumnMeta, kind Kind) (ColumnType, error) {
	if n, ok := c.MaxLength(); ok {
		if kind != KindText {
			return ColumnType{}, fmt.Errorf("column %q: MaxLen is only valid on string columns", c.Name())
		}
		if n <= 0 {
			return ColumnType{}, fmt.Errorf("column %q: MaxLen must be positive, got %d", c.Name(), n)
		}
		return ColumnType{Kind: KindVarchar, Length: n}, nil
	}

	if p, s, ok := c.NumericPrecisionScale(); ok {
		if kind != KindNumeric {
			return ColumnType{}, fmt.Errorf("column %q: Numeric is only valid on numeric columns", c.Name())
		}
		if err := validateNumericPrecisionScale(c.Name(), p, s); err != nil {
			return ColumnType{}, err
		}
		return ColumnType{Kind: KindNumeric, Precision: p, Scale: s}, nil
	}

	return ColumnType{Kind: kind}, nil
}

// validateNumericPrecisionScale validates the values passed to Numeric,
// shared between scalarColumnType and arrayColumnType.
func validateNumericPrecisionScale(column string, precision, scale int) error {
	if precision <= 0 {
		return fmt.Errorf("column %q: Numeric precision must be positive, got %d", column, precision)
	}
	if scale < 0 {
		return fmt.Errorf("column %q: Numeric scale must not be negative, got %d", column, scale)
	}
	if scale > precision {
		return fmt.Errorf("column %q: Numeric scale (%d) must not exceed precision (%d)", column, scale, precision)
	}
	return nil
}
