package postgres

import (
	"context"
	"fmt"

	"github.com/tork-go/orm/driver"
	"github.com/tork-go/orm/schema"
)

const columnsQuery = `
SELECT c.table_name, c.column_name, c.data_type, c.udt_name,
       c.character_maximum_length, c.numeric_precision, c.numeric_scale,
       c.is_nullable
FROM information_schema.columns c
WHERE c.table_schema = 'public' AND c.table_name = ANY($1)
ORDER BY c.table_name, c.ordinal_position`

// enumValuesQuery reads the declared values, in order, of exactly the
// enum types named in $1 (the USER-DEFINED udt_names seen while scanning
// columnsQuery), never every enum type in the database, matching the
// same table-scoping rationale documented for the rest of introspection:
// introspecting an unrelated enum type would risk a phantom DropEnumType.
const enumValuesQuery = `
SELECT t.typname, e.enumlabel
FROM pg_type t
JOIN pg_enum e ON e.enumtypid = t.oid
JOIN pg_namespace n ON n.oid = t.typnamespace
WHERE n.nspname = 'public' AND t.typname = ANY($1)
ORDER BY t.typname, e.enumsortorder`

// arrayColumnTypesQuery reads every column's fully formatted type via
// Postgres's format_type() builtin (e.g. "character varying(50)[]",
// "numeric(10,2)[]", "integer[]"), the only way to recover an array
// column's element length/precision: information_schema.columns exposes
// none of that for an ARRAY-typed column. introspectColumns only uses the
// rows for columns it already knows are arrays from columnsQuery.
const arrayColumnTypesQuery = `
SELECT c.relname AS table_name, a.attname AS column_name,
       format_type(a.atttypid, a.atttypmod) AS formatted_type
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relname = ANY($1)
  AND a.attnum > 0 AND NOT a.attisdropped`

const constraintColumnsQuery = `
SELECT tc.table_name, tc.constraint_name, kcu.column_name, kcu.ordinal_position
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
WHERE tc.constraint_type = $2 AND tc.table_schema = 'public' AND tc.table_name = ANY($1)
ORDER BY tc.table_name, tc.constraint_name, kcu.ordinal_position`

// foreignKeyColumnsQuery finds each foreign key's own columns, the
// table/column it references, and its ON UPDATE/ON DELETE actions. As
// documented in package doc.go, the join shape for the referenced side
// does not reliably preserve ordinal alignment for composite foreign
// keys; round 1's model API only ever produces single-column foreign
// keys, for which this is correct.
const foreignKeyColumnsQuery = `
SELECT tc.table_name, tc.constraint_name, kcu.column_name, kcu.ordinal_position,
       ccu.table_name AS referenced_table, ccu.column_name AS referenced_column,
       rc.update_rule, rc.delete_rule
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
JOIN information_schema.constraint_column_usage ccu
  ON tc.constraint_name = ccu.constraint_name AND tc.table_schema = ccu.table_schema
JOIN information_schema.referential_constraints rc
  ON rc.constraint_name = tc.constraint_name AND rc.constraint_schema = tc.table_schema
WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = 'public' AND tc.table_name = ANY($1)
ORDER BY tc.table_name, tc.constraint_name, kcu.ordinal_position`

// indexColumnsQuery finds every plain (non-unique, non-primary-key) index
// and its columns in order. information_schema has no view for this, so
// it needs pg_index directly. As documented in package doc.go, expression
// indexes and partial indexes are excluded, not misrepresented:
// indexprs IS NULL/indpred IS NULL filter both out. Without the indexprs
// filter, a mixed index like (col_a, lower(col_b)) would silently
// truncate to a wrong single-column index, since an expression key
// position has attnum = 0, matching no real column in the join below;
// excluding it outright is correct, guessing at it would not be. A
// partial index's WHERE predicate has no representation in schema.Index
// at all, so introspecting one without it would let Diff treat it as
// equivalent to a full index.
const indexColumnsQuery = `
SELECT t.relname AS table_name, i.relname AS index_name, a.attname AS column_name, k.ord AS ordinal_position
FROM pg_index ix
JOIN pg_class t ON t.oid = ix.indrelid
JOIN pg_class i ON i.oid = ix.indexrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
JOIN LATERAL unnest(ix.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord) ON true
JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum AND NOT a.attisdropped
WHERE n.nspname = 'public' AND t.relname = ANY($1)
  AND NOT ix.indisunique AND NOT ix.indisprimary
  AND ix.indexprs IS NULL AND ix.indpred IS NULL
ORDER BY t.relname, i.relname, k.ord`

// checksQuery finds every CHECK constraint and its expression text.
// pg_get_expr(conbin, conrelid) returns the bare expression (no
// surrounding CHECK (...)), which is what makes a text comparison against
// a user-authored expression viable at all. The join to pg_class excludes
// domain-level checks (conrelid = 0 has no matching row).
const checksQuery = `
SELECT t.relname AS table_name, co.conname AS constraint_name,
       pg_get_expr(co.conbin, co.conrelid) AS expression
FROM pg_constraint co
JOIN pg_class t ON t.oid = co.conrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
WHERE n.nspname = 'public' AND t.relname = ANY($1) AND co.contype = 'c'
ORDER BY t.relname, co.conname`

// Introspect reads the current schema for exactly the given tables. Tables
// that don't exist yet are simply absent from the result, not an error.
func (d Dialect) Introspect(ctx context.Context, conn driver.Conn, tables []string) (schema.Schema, error) {
	if len(tables) == 0 {
		return schema.Schema{}, nil
	}

	byTable := map[string]*schema.Table{}
	order := make([]string, 0, len(tables))
	table := func(name string) *schema.Table {
		t, ok := byTable[name]
		if !ok {
			t = &schema.Table{Name: name}
			byTable[name] = t
			order = append(order, name)
		}
		return t
	}

	enumTypes, err := introspectColumns(ctx, conn, tables, table)
	if err != nil {
		return schema.Schema{}, err
	}
	if err := introspectPrimaryKeys(ctx, conn, tables, table); err != nil {
		return schema.Schema{}, err
	}
	if err := introspectUniques(ctx, conn, tables, table); err != nil {
		return schema.Schema{}, err
	}
	if err := introspectIndexes(ctx, conn, tables, table); err != nil {
		return schema.Schema{}, err
	}
	if err := introspectChecks(ctx, conn, tables, table); err != nil {
		return schema.Schema{}, err
	}
	if err := introspectForeignKeys(ctx, conn, tables, table); err != nil {
		return schema.Schema{}, err
	}

	var s schema.Schema
	s.EnumTypes = enumTypes
	for _, name := range order {
		s.Tables = append(s.Tables, *byTable[name])
	}
	return s, nil
}

// rawColumnRow is one scanned row of columnsQuery, before its final
// schema.ColumnType is resolved. Resolution needs two extra passes for
// ARRAY and USER-DEFINED (enum) columns, so every row is collected first.
type rawColumnRow struct {
	TableName  string
	ColumnName string
	Raw        rawColumnType
	IsNullable string
}

// introspectColumns reads every column across tables in three passes:
// first the raw information_schema.columns rows, then (only if needed)
// enum values for any USER-DEFINED columns and formatted types for any
// ARRAY columns, then each row's final schema.ColumnType is resolved and
// appended. It returns the enum types discovered along the way, deduped
// by name (a type used by several columns appears once).
func introspectColumns(ctx context.Context, conn driver.Conn, tables []string, table func(string) *schema.Table) ([]schema.EnumType, error) {
	rows, err := conn.Query(ctx, columnsQuery, tables)
	if err != nil {
		return nil, fmt.Errorf("postgres: introspecting columns: %w", err)
	}

	var raws []rawColumnRow
	enumTypeNames := map[string]bool{}
	arrayColumns := map[string]bool{} // "table.column"
	for rows.Next() {
		var r rawColumnRow
		if err := rows.Scan(&r.TableName, &r.ColumnName, &r.Raw.DataType, &r.Raw.UDTName,
			&r.Raw.CharMaxLen, &r.Raw.NumericPrecision, &r.Raw.NumericScale, &r.IsNullable); err != nil {
			rows.Close()
			return nil, fmt.Errorf("postgres: scanning column: %w", err)
		}
		raws = append(raws, r)
		switch r.Raw.DataType {
		case "USER-DEFINED":
			enumTypeNames[r.Raw.UDTName] = true
		case "ARRAY":
			arrayColumns[r.TableName+"."+r.ColumnName] = true
		}
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}

	enumValues, enumTypes, err := introspectEnumValues(ctx, conn, enumTypeNames)
	if err != nil {
		return nil, err
	}
	arrayTypes, err := introspectArrayFormatTypes(ctx, conn, tables, arrayColumns)
	if err != nil {
		return nil, err
	}

	for _, r := range raws {
		colType, err := resolveColumnType(r, enumValues, arrayTypes)
		if err != nil {
			return nil, fmt.Errorf("postgres: table %q column %q: %w", r.TableName, r.ColumnName, err)
		}
		t := table(r.TableName)
		t.Columns = append(t.Columns, schema.Column{
			Name:    r.ColumnName,
			Type:    colType,
			NotNull: r.IsNullable == "NO",
		})
	}
	return enumTypes, nil
}

// resolveColumnType resolves one rawColumnRow's final schema.ColumnType,
// using the auxiliary enum/array lookups for the two data_type values
// parseType can't handle alone.
func resolveColumnType(r rawColumnRow, enumValues map[string][]string, arrayTypes map[string]string) (schema.ColumnType, error) {
	switch r.Raw.DataType {
	case "ARRAY":
		formatted, ok := arrayTypes[r.TableName+"."+r.ColumnName]
		if !ok {
			return schema.ColumnType{}, fmt.Errorf("array column has no formatted type")
		}
		return parseArrayFormatType(formatted)
	case "USER-DEFINED":
		if _, ok := enumValues[r.Raw.UDTName]; !ok {
			return schema.ColumnType{}, fmt.Errorf("unsupported user-defined type %q (not an enum Tork recognizes)", r.Raw.UDTName)
		}
		return schema.ColumnType{Kind: schema.KindEnum, TypeName: r.Raw.UDTName}, nil
	default:
		return parseType(r.Raw)
	}
}

// introspectEnumValues reads the declared values of exactly the enum type
// names in typeNames, returning both a name-to-values lookup (used to
// resolve individual columns) and the deduped []schema.EnumType list
// (used to populate schema.Schema.EnumTypes).
func introspectEnumValues(ctx context.Context, conn driver.Conn, typeNames map[string]bool) (map[string][]string, []schema.EnumType, error) {
	if len(typeNames) == 0 {
		return map[string][]string{}, nil, nil
	}
	names := make([]string, 0, len(typeNames))
	for n := range typeNames {
		names = append(names, n)
	}

	rows, err := conn.Query(ctx, enumValuesQuery, names)
	if err != nil {
		return nil, nil, fmt.Errorf("postgres: introspecting enum values: %w", err)
	}
	defer rows.Close()

	values := map[string][]string{}
	var order []string
	for rows.Next() {
		var typeName, label string
		if err := rows.Scan(&typeName, &label); err != nil {
			return nil, nil, fmt.Errorf("postgres: scanning enum value: %w", err)
		}
		if _, ok := values[typeName]; !ok {
			order = append(order, typeName)
		}
		values[typeName] = append(values[typeName], label)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	enumTypes := make([]schema.EnumType, len(order))
	for i, name := range order {
		enumTypes[i] = schema.EnumType{Name: name, Values: values[name]}
	}
	return values, enumTypes, nil
}

// introspectArrayFormatTypes reads format_type() for every column of
// tables and returns only the entries arrayColumns asks for, keyed
// "table.column".
func introspectArrayFormatTypes(ctx context.Context, conn driver.Conn, tables []string, arrayColumns map[string]bool) (map[string]string, error) {
	if len(arrayColumns) == 0 {
		return map[string]string{}, nil
	}

	rows, err := conn.Query(ctx, arrayColumnTypesQuery, tables)
	if err != nil {
		return nil, fmt.Errorf("postgres: introspecting array column types: %w", err)
	}
	defer rows.Close()

	formatted := map[string]string{}
	for rows.Next() {
		var tableName, columnName, formattedType string
		if err := rows.Scan(&tableName, &columnName, &formattedType); err != nil {
			return nil, fmt.Errorf("postgres: scanning array column type: %w", err)
		}
		key := tableName + "." + columnName
		if arrayColumns[key] {
			formatted[key] = formattedType
		}
	}
	return formatted, rows.Err()
}

func introspectPrimaryKeys(ctx context.Context, conn driver.Conn, tables []string, table func(string) *schema.Table) error {
	rows, err := conn.Query(ctx, constraintColumnsQuery, tables, "PRIMARY KEY")
	if err != nil {
		return fmt.Errorf("postgres: introspecting primary keys: %w", err)
	}
	defer rows.Close()

	pks := map[string]*schema.PrimaryKey{}
	for rows.Next() {
		var tableName, constraintName, columnName string
		var ordinal int
		if err := rows.Scan(&tableName, &constraintName, &columnName, &ordinal); err != nil {
			return fmt.Errorf("postgres: scanning primary key: %w", err)
		}
		pk, ok := pks[tableName]
		if !ok {
			pk = &schema.PrimaryKey{Name: constraintName}
			pks[tableName] = pk
			table(tableName).PrimaryKey = pk
		}
		pk.Columns = append(pk.Columns, columnName)
	}
	return rows.Err()
}

func introspectUniques(ctx context.Context, conn driver.Conn, tables []string, table func(string) *schema.Table) error {
	rows, err := conn.Query(ctx, constraintColumnsQuery, tables, "UNIQUE")
	if err != nil {
		return fmt.Errorf("postgres: introspecting unique constraints: %w", err)
	}
	defer rows.Close()

	// Indices, not pointers, into each table's Uniques slice: appending
	// more constraints for the same table can reallocate that slice, which
	// would leave a cached pointer dangling.
	indices := map[string]int{}
	for rows.Next() {
		var tableName, constraintName, columnName string
		var ordinal int
		if err := rows.Scan(&tableName, &constraintName, &columnName, &ordinal); err != nil {
			return fmt.Errorf("postgres: scanning unique constraint: %w", err)
		}
		t := table(tableName)
		key := tableName + "." + constraintName
		idx, ok := indices[key]
		if !ok {
			t.Uniques = append(t.Uniques, schema.UniqueConstraint{Name: constraintName})
			idx = len(t.Uniques) - 1
			indices[key] = idx
		}
		t.Uniques[idx].Columns = append(t.Uniques[idx].Columns, columnName)
	}
	return rows.Err()
}

func introspectIndexes(ctx context.Context, conn driver.Conn, tables []string, table func(string) *schema.Table) error {
	rows, err := conn.Query(ctx, indexColumnsQuery, tables)
	if err != nil {
		return fmt.Errorf("postgres: introspecting indexes: %w", err)
	}
	defer rows.Close()

	// Indices, not pointers, into each table's Indexes slice: see the same
	// note on introspectUniques above.
	indices := map[string]int{}
	for rows.Next() {
		var tableName, indexName, columnName string
		var ordinal int
		if err := rows.Scan(&tableName, &indexName, &columnName, &ordinal); err != nil {
			return fmt.Errorf("postgres: scanning index: %w", err)
		}
		t := table(tableName)
		key := tableName + "." + indexName
		idx, ok := indices[key]
		if !ok {
			t.Indexes = append(t.Indexes, schema.Index{Name: indexName})
			idx = len(t.Indexes) - 1
			indices[key] = idx
		}
		t.Indexes[idx].Columns = append(t.Indexes[idx].Columns, columnName)
	}
	return rows.Err()
}

func introspectChecks(ctx context.Context, conn driver.Conn, tables []string, table func(string) *schema.Table) error {
	rows, err := conn.Query(ctx, checksQuery, tables)
	if err != nil {
		return fmt.Errorf("postgres: introspecting check constraints: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tableName, constraintName, expression string
		if err := rows.Scan(&tableName, &constraintName, &expression); err != nil {
			return fmt.Errorf("postgres: scanning check constraint: %w", err)
		}
		t := table(tableName)
		t.Checks = append(t.Checks, schema.Check{Name: constraintName, Expression: expression})
	}
	return rows.Err()
}

func introspectForeignKeys(ctx context.Context, conn driver.Conn, tables []string, table func(string) *schema.Table) error {
	rows, err := conn.Query(ctx, foreignKeyColumnsQuery, tables)
	if err != nil {
		return fmt.Errorf("postgres: introspecting foreign keys: %w", err)
	}
	defer rows.Close()

	fks := map[string]int{} // "table.constraint" -> index into that table's ForeignKeys slice
	for rows.Next() {
		var tableName, constraintName, columnName, refTable, refColumn, updateRule, deleteRule string
		var ordinal int
		if err := rows.Scan(&tableName, &constraintName, &columnName, &ordinal,
			&refTable, &refColumn, &updateRule, &deleteRule); err != nil {
			return fmt.Errorf("postgres: scanning foreign key: %w", err)
		}
		t := table(tableName)
		key := tableName + "." + constraintName
		idx, ok := fks[key]
		if !ok {
			onUpdate, err := parseAction(updateRule)
			if err != nil {
				return fmt.Errorf("postgres: table %q constraint %q: %w", tableName, constraintName, err)
			}
			onDelete, err := parseAction(deleteRule)
			if err != nil {
				return fmt.Errorf("postgres: table %q constraint %q: %w", tableName, constraintName, err)
			}
			t.ForeignKeys = append(t.ForeignKeys, schema.ForeignKey{
				Name:            constraintName,
				ReferencedTable: refTable,
				OnUpdate:        onUpdate,
				OnDelete:        onDelete,
			})
			idx = len(t.ForeignKeys) - 1
			fks[key] = idx
		}
		t.ForeignKeys[idx].Columns = append(t.ForeignKeys[idx].Columns, columnName)
		t.ForeignKeys[idx].ReferencedColumns = append(t.ForeignKeys[idx].ReferencedColumns, refColumn)
	}
	return rows.Err()
}
