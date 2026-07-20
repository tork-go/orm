package postgres

import (
	"context"
	"fmt"

	"github.com/tork-go/orm/driver"
	"github.com/tork-go/orm/schema"
)

const columnsQuery = `
SELECT c.table_name, c.column_name, c.data_type, c.character_maximum_length, c.is_nullable
FROM information_schema.columns c
WHERE c.table_schema = 'public' AND c.table_name = ANY($1)
ORDER BY c.table_name, c.ordinal_position`

const constraintColumnsQuery = `
SELECT tc.table_name, tc.constraint_name, kcu.column_name, kcu.ordinal_position
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
WHERE tc.constraint_type = $2 AND tc.table_schema = 'public' AND tc.table_name = ANY($1)
ORDER BY tc.table_name, tc.constraint_name, kcu.ordinal_position`

// foreignKeyColumnsQuery finds each foreign key's own columns plus the
// table/column it references. As documented in package doc.go, this join
// shape does not reliably preserve ordinal alignment for composite foreign
// keys; round 1's model API only ever produces single-column foreign keys,
// for which this is correct.
const foreignKeyColumnsQuery = `
SELECT tc.table_name, tc.constraint_name, kcu.column_name, kcu.ordinal_position,
       ccu.table_name AS referenced_table, ccu.column_name AS referenced_column
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
JOIN information_schema.constraint_column_usage ccu
  ON tc.constraint_name = ccu.constraint_name AND tc.table_schema = ccu.table_schema
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

	if err := introspectColumns(ctx, conn, tables, table); err != nil {
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
	if err := introspectForeignKeys(ctx, conn, tables, table); err != nil {
		return schema.Schema{}, err
	}

	var s schema.Schema
	for _, name := range order {
		s.Tables = append(s.Tables, *byTable[name])
	}
	return s, nil
}

func introspectColumns(ctx context.Context, conn driver.Conn, tables []string, table func(string) *schema.Table) error {
	rows, err := conn.Query(ctx, columnsQuery, tables)
	if err != nil {
		return fmt.Errorf("postgres: introspecting columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tableName, columnName, dataType, isNullable string
		var charMaxLen *int
		if err := rows.Scan(&tableName, &columnName, &dataType, &charMaxLen, &isNullable); err != nil {
			return fmt.Errorf("postgres: scanning column: %w", err)
		}
		colType, err := parseType(dataType, charMaxLen)
		if err != nil {
			return fmt.Errorf("postgres: table %q column %q: %w", tableName, columnName, err)
		}
		t := table(tableName)
		t.Columns = append(t.Columns, schema.Column{
			Name:    columnName,
			Type:    colType,
			NotNull: isNullable == "NO",
		})
	}
	return rows.Err()
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

func introspectForeignKeys(ctx context.Context, conn driver.Conn, tables []string, table func(string) *schema.Table) error {
	rows, err := conn.Query(ctx, foreignKeyColumnsQuery, tables)
	if err != nil {
		return fmt.Errorf("postgres: introspecting foreign keys: %w", err)
	}
	defer rows.Close()

	fks := map[string]int{} // "table.constraint" -> index into that table's ForeignKeys slice
	for rows.Next() {
		var tableName, constraintName, columnName, refTable, refColumn string
		var ordinal int
		if err := rows.Scan(&tableName, &constraintName, &columnName, &ordinal, &refTable, &refColumn); err != nil {
			return fmt.Errorf("postgres: scanning foreign key: %w", err)
		}
		t := table(tableName)
		key := tableName + "." + constraintName
		idx, ok := fks[key]
		if !ok {
			t.ForeignKeys = append(t.ForeignKeys, schema.ForeignKey{
				Name:            constraintName,
				ReferencedTable: refTable,
			})
			idx = len(t.ForeignKeys) - 1
			fks[key] = idx
		}
		t.ForeignKeys[idx].Columns = append(t.ForeignKeys[idx].Columns, columnName)
		t.ForeignKeys[idx].ReferencedColumns = append(t.ForeignKeys[idx].ReferencedColumns, refColumn)
	}
	return rows.Err()
}
