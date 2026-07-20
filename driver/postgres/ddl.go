package postgres

import (
	"fmt"
	"strings"

	"github.com/tork-go/orm/schema"
)

// quoteIdent double-quotes a Postgres identifier, escaping any embedded
// double quotes.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quoteIdentList double-quotes and comma-joins a list of identifiers.
func quoteIdentList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = quoteIdent(n)
	}
	return strings.Join(quoted, ", ")
}

// RenderCreateTable renders a CREATE TABLE statement. A single-column
// primary key on an integer or bigint column is rendered inline as an
// auto-incrementing identity column; any other primary key (composite, or
// a single non-integer column) is a table-level constraint. Unique
// constraints are always table-level. Foreign keys are never included
// here, they are always added separately (see driver.Dialect's doc
// comment on RenderCreateTable's siblings).
func (Dialect) RenderCreateTable(t schema.Table) ([]string, error) {
	identityColumn := ""
	if t.PrimaryKey != nil && len(t.PrimaryKey.Columns) == 1 {
		name := t.PrimaryKey.Columns[0]
		for _, c := range t.Columns {
			if c.Name == name && (c.Type.Kind == schema.KindInteger || c.Type.Kind == schema.KindBigInteger) {
				identityColumn = name
			}
		}
	}

	var lines []string
	for _, c := range t.Columns {
		typ, err := renderType(c.Type)
		if err != nil {
			return nil, fmt.Errorf("table %q: %w", t.Name, err)
		}
		line := fmt.Sprintf("%s %s", quoteIdent(c.Name), typ)
		switch {
		case c.Name == identityColumn:
			line += " GENERATED ALWAYS AS IDENTITY PRIMARY KEY"
		case c.NotNull:
			line += " NOT NULL"
		}
		lines = append(lines, line)
	}

	if t.PrimaryKey != nil && identityColumn == "" {
		lines = append(lines, fmt.Sprintf("CONSTRAINT %s PRIMARY KEY (%s)",
			quoteIdent(t.PrimaryKey.Name), quoteIdentList(t.PrimaryKey.Columns)))
	}
	for _, u := range t.Uniques {
		lines = append(lines, fmt.Sprintf("CONSTRAINT %s UNIQUE (%s)",
			quoteIdent(u.Name), quoteIdentList(u.Columns)))
	}

	sql := fmt.Sprintf("CREATE TABLE %s (\n    %s\n)", quoteIdent(t.Name), strings.Join(lines, ",\n    "))
	return []string{sql}, nil
}

// RenderDropTable renders a DROP TABLE statement.
func (Dialect) RenderDropTable(table string) []string {
	return []string{fmt.Sprintf("DROP TABLE %s", quoteIdent(table))}
}

// RenderAddColumn renders an ALTER TABLE ... ADD COLUMN statement.
func (Dialect) RenderAddColumn(table string, col schema.Column) ([]string, error) {
	typ, err := renderType(col.Type)
	if err != nil {
		return nil, fmt.Errorf("table %q: %w", table, err)
	}
	sql := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", quoteIdent(table), quoteIdent(col.Name), typ)
	if col.NotNull {
		sql += " NOT NULL"
	}
	return []string{sql}, nil
}

// RenderDropColumn renders an ALTER TABLE ... DROP COLUMN statement.
func (Dialect) RenderDropColumn(table, column string) []string {
	return []string{fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", quoteIdent(table), quoteIdent(column))}
}

// RenderAlterColumnType renders an ALTER TABLE ... ALTER COLUMN ... TYPE
// statement.
func (Dialect) RenderAlterColumnType(table string, col schema.Column) ([]string, error) {
	typ, err := renderType(col.Type)
	if err != nil {
		return nil, fmt.Errorf("table %q: %w", table, err)
	}
	sql := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", quoteIdent(table), quoteIdent(col.Name), typ)
	return []string{sql}, nil
}

// RenderAlterColumnNullability renders an ALTER TABLE ... ALTER COLUMN ...
// SET/DROP NOT NULL statement.
func (Dialect) RenderAlterColumnNullability(table, column string, notNull bool) []string {
	verb := "DROP NOT NULL"
	if notNull {
		verb = "SET NOT NULL"
	}
	sql := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s", quoteIdent(table), quoteIdent(column), verb)
	return []string{sql}
}

// RenderAddPrimaryKey renders an ALTER TABLE ... ADD CONSTRAINT ... PRIMARY
// KEY statement.
func (Dialect) RenderAddPrimaryKey(table string, pk schema.PrimaryKey) []string {
	sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s PRIMARY KEY (%s)",
		quoteIdent(table), quoteIdent(pk.Name), quoteIdentList(pk.Columns))
	return []string{sql}
}

// RenderDropPrimaryKey renders an ALTER TABLE ... DROP CONSTRAINT statement.
func (Dialect) RenderDropPrimaryKey(table, name string) []string {
	return []string{fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", quoteIdent(table), quoteIdent(name))}
}

// RenderAddUnique renders an ALTER TABLE ... ADD CONSTRAINT ... UNIQUE
// statement.
func (Dialect) RenderAddUnique(table string, u schema.UniqueConstraint) []string {
	sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s)",
		quoteIdent(table), quoteIdent(u.Name), quoteIdentList(u.Columns))
	return []string{sql}
}

// RenderDropUnique renders an ALTER TABLE ... DROP CONSTRAINT statement.
func (Dialect) RenderDropUnique(table, name string) []string {
	return []string{fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", quoteIdent(table), quoteIdent(name))}
}

// RenderAddForeignKey renders an ALTER TABLE ... ADD CONSTRAINT ... FOREIGN
// KEY statement.
func (Dialect) RenderAddForeignKey(table string, fk schema.ForeignKey) []string {
	sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
		quoteIdent(table), quoteIdent(fk.Name), quoteIdentList(fk.Columns),
		quoteIdent(fk.ReferencedTable), quoteIdentList(fk.ReferencedColumns))
	return []string{sql}
}

// RenderDropForeignKey renders an ALTER TABLE ... DROP CONSTRAINT
// statement.
func (Dialect) RenderDropForeignKey(table, name string) []string {
	return []string{fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", quoteIdent(table), quoteIdent(name))}
}
