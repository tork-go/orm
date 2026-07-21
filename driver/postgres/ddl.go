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

// quoteLiteral single-quotes a Postgres string literal, escaping any
// embedded single quotes.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
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
			// schema.ExtractSchema already rejects a ServerDefault on an
			// identity column (Postgres disallows both GENERATED ALWAYS AS
			// IDENTITY and an explicit DEFAULT on the same column), but
			// this stays safe even against a hand-built schema.Table that
			// skipped that validation, by simply not looking at
			// ServerDefault here.
			line += " GENERATED ALWAYS AS IDENTITY PRIMARY KEY"
		default:
			if c.ServerDefault != "" {
				line += " DEFAULT " + c.ServerDefault
			}
			if c.NotNull {
				line += " NOT NULL"
			}
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
	for _, ck := range t.Checks {
		lines = append(lines, fmt.Sprintf("CONSTRAINT %s CHECK (%s)", quoteIdent(ck.Name), ck.Expression))
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
	if col.ServerDefault != "" {
		sql += " DEFAULT " + col.ServerDefault
	}
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

// RenderAlterColumnDefault sets a column's DEFAULT clause, or drops it
// when def is empty.
func (Dialect) RenderAlterColumnDefault(table, column, def string) []string {
	if def == "" {
		return []string{fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT",
			quoteIdent(table), quoteIdent(column))}
	}
	return []string{fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s",
		quoteIdent(table), quoteIdent(column), def)}
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

// RenderAddIndex renders a CREATE INDEX statement. Unlike a constraint, a
// plain index cannot be declared inline in CREATE TABLE in Postgres, so it
// is always its own statement.
func (Dialect) RenderAddIndex(table string, idx schema.Index) []string {
	sql := fmt.Sprintf("CREATE INDEX %s ON %s (%s)",
		quoteIdent(idx.Name), quoteIdent(table), quoteIdentList(idx.Columns))
	return []string{sql}
}

// RenderDropIndex renders a DROP INDEX statement. table is accepted for
// consistency with every other Drop* method here; Postgres itself doesn't
// need it, since index names are schema-scoped, not table-scoped.
func (Dialect) RenderDropIndex(table, name string) []string {
	return []string{fmt.Sprintf("DROP INDEX %s", quoteIdent(name))}
}

// RenderAddCheck renders an ALTER TABLE ... ADD CONSTRAINT ... CHECK
// statement.
func (Dialect) RenderAddCheck(table string, c schema.Check) []string {
	sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)",
		quoteIdent(table), quoteIdent(c.Name), c.Expression)
	return []string{sql}
}

// RenderDropCheck renders an ALTER TABLE ... DROP CONSTRAINT statement.
func (Dialect) RenderDropCheck(table, name string) []string {
	return []string{fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", quoteIdent(table), quoteIdent(name))}
}

// RenderAddForeignKey renders an ALTER TABLE ... ADD CONSTRAINT ... FOREIGN
// KEY statement, with ON DELETE/ON UPDATE clauses only when non-default.
func (Dialect) RenderAddForeignKey(table string, fk schema.ForeignKey) []string {
	sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
		quoteIdent(table), quoteIdent(fk.Name), quoteIdentList(fk.Columns),
		quoteIdent(fk.ReferencedTable), quoteIdentList(fk.ReferencedColumns))
	if c := actionClause("ON DELETE", fk.OnDelete); c != "" {
		sql += " " + c
	}
	if c := actionClause("ON UPDATE", fk.OnUpdate); c != "" {
		sql += " " + c
	}
	return []string{sql}
}

// actionClause renders a referential action clause, or "" for
// schema.ActionNoAction, Postgres's own default (no clause needed).
func actionClause(prefix string, a schema.ForeignKeyAction) string {
	switch a {
	case schema.ActionCascade:
		return prefix + " CASCADE"
	case schema.ActionSetNull:
		return prefix + " SET NULL"
	case schema.ActionSetDefault:
		return prefix + " SET DEFAULT"
	case schema.ActionRestrict:
		return prefix + " RESTRICT"
	default:
		return ""
	}
}

// RenderDropForeignKey renders an ALTER TABLE ... DROP CONSTRAINT
// statement.
func (Dialect) RenderDropForeignKey(table, name string) []string {
	return []string{fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", quoteIdent(table), quoteIdent(name))}
}

// RenderCreateEnumType renders a CREATE TYPE ... AS ENUM statement.
func (Dialect) RenderCreateEnumType(e schema.EnumType) []string {
	values := make([]string, len(e.Values))
	for i, v := range e.Values {
		values[i] = quoteLiteral(v)
	}
	sql := fmt.Sprintf("CREATE TYPE %s AS ENUM (%s)", quoteIdent(e.Name), strings.Join(values, ", "))
	return []string{sql}
}

// RenderDropEnumType renders a DROP TYPE statement.
func (Dialect) RenderDropEnumType(name string) []string {
	return []string{fmt.Sprintf("DROP TYPE %s", quoteIdent(name))}
}

// RenderAddEnumValue renders an ALTER TYPE ... ADD VALUE statement,
// optionally positioned via before/after (mutually exclusive; both empty
// appends the value at the end of the type's value list).
func (Dialect) RenderAddEnumValue(name, value, before, after string) []string {
	sql := fmt.Sprintf("ALTER TYPE %s ADD VALUE %s", quoteIdent(name), quoteLiteral(value))
	switch {
	case before != "":
		sql += " BEFORE " + quoteLiteral(before)
	case after != "":
		sql += " AFTER " + quoteLiteral(after)
	}
	return []string{sql}
}
