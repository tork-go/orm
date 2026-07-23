package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tork-go/orm/gen/analyze"
)

// modelFile renders one model's complete file: row struct, model
// struct, the optional constraint methods, and the DefineTable
// declaration, in the exact shape a handwritten model takes so that
// generated and handwritten models are indistinguishable to both the
// ORM and a reader.
func (g *generator) modelFile(m *analyze.Model) []byte {
	imp := newImports()
	imp.add("github.com/tork-go/orm")

	var body strings.Builder
	g.printRowStruct(&body, imp, m)
	g.printModelStruct(&body, imp, m)
	g.printIndexes(&body, m)
	g.printChecks(&body, m)
	g.printForeignKeys(&body, m)
	g.printRelations(&body, m)
	g.printDefineTable(&body, imp, m)

	var out strings.Builder
	out.WriteString(header)
	out.WriteString("package " + g.pkg + "\n\n")
	out.WriteString(imp.render())
	out.WriteString(body.String())
	return []byte(out.String())
}

// columnFields and relationFields split a model's fields into the two
// groups every section renders separately, keeping declaration order
// within each.
func columnFields(m *analyze.Model) []*analyze.Field {
	var out []*analyze.Field
	for _, f := range m.Fields {
		if f.Relation == nil {
			out = append(out, f)
		}
	}
	return out
}

func relationFields(m *analyze.Model) []*analyze.Field {
	var out []*analyze.Field
	for _, f := range m.Fields {
		if f.Relation != nil {
			out = append(out, f)
		}
	}
	return out
}

func (g *generator) printRowStruct(b *strings.Builder, imp *imports, m *analyze.Model) {
	b.WriteString(docComment(m.Doc, fmt.Sprintf("%s is the row type of the %s table.", m.Name, m.TableName)))
	fmt.Fprintf(b, "type %s struct {\n", m.Name)
	for _, f := range columnFields(m) {
		if f.Doc != "" {
			for _, line := range strings.Split(f.Doc, "\n") {
				b.WriteString("\t// " + line + "\n")
			}
		}
		fmt.Fprintf(b, "\t%s %s `db:%q`\n", f.GoName, g.rowType(imp, f), f.ColumnName)
	}
	if rels := relationFields(m); len(rels) > 0 {
		b.WriteString("\n")
		for _, f := range rels {
			fmt.Fprintf(b, "\t%s %s\n", f.GoName, rowRelationType(f))
		}
	}
	b.WriteString("}\n\n")
}

func (g *generator) printModelStruct(b *strings.Builder, imp *imports, m *analyze.Model) {
	fmt.Fprintf(b, "// %sModel declares the %s table.\n", m.Name, m.TableName)
	fmt.Fprintf(b, "type %sModel struct {\n", m.Name)
	fmt.Fprintf(b, "\torm.Table[%s]\n", m.Name)
	for _, f := range columnFields(m) {
		fmt.Fprintf(b, "\t%s %s\n", f.GoName, g.columnType(imp, f))
	}
	for _, f := range relationFields(m) {
		fmt.Fprintf(b, "\t%s %s\n", f.GoName, markerType(f))
	}
	b.WriteString("}\n\n")
}

func (g *generator) printIndexes(b *strings.Builder, m *analyze.Model) {
	if len(m.Indexes) == 0 {
		return
	}
	b.WriteString("// Indexes implements orm.Indexer.\n")
	fmt.Fprintf(b, "func (m *%sModel) Indexes() []orm.IndexDef {\n", m.Name)
	b.WriteString("\treturn []orm.IndexDef{\n")
	for _, idx := range m.Indexes {
		cols := make([]string, len(idx.Fields))
		for i, f := range idx.Fields {
			cols[i] = "m." + f.GoName
		}
		entry := "orm.NewIndexDef(" + strings.Join(cols, ", ") + ")"
		if idx.Unique {
			entry += ".Unique()"
		}
		if len(idx.Expressions) > 0 {
			exprs := make([]string, len(idx.Expressions))
			for i, e := range idx.Expressions {
				exprs[i] = strconv.Quote(e)
			}
			entry += ".On(" + strings.Join(exprs, ", ") + ")"
		}
		if idx.Where != "" {
			entry += ".Where(" + strconv.Quote(idx.Where) + ")"
		}
		if idx.Name != "" {
			entry += ".Named(" + strconv.Quote(idx.Name) + ")"
		}
		b.WriteString("\t\t" + entry + ",\n")
	}
	b.WriteString("\t}\n}\n\n")
}

func (g *generator) printChecks(b *strings.Builder, m *analyze.Model) {
	if len(m.Checks) == 0 {
		return
	}
	b.WriteString("// Checks implements orm.Checker.\n")
	fmt.Fprintf(b, "func (m *%sModel) Checks() []orm.CheckDef {\n", m.Name)
	b.WriteString("\treturn []orm.CheckDef{\n")
	for _, c := range m.Checks {
		entry := "orm.NewCheckDef(" + strconv.Quote(c.Expression) + ")"
		if c.Name != "" {
			entry += ".Named(" + strconv.Quote(c.Name) + ")"
		}
		b.WriteString("\t\t" + entry + ",\n")
	}
	b.WriteString("\t}\n}\n\n")
}

// methodForeignKeys lists the belongs to relations that render inside
// a ForeignKeys method instead of on their column: composite keys and
// named constraints, which the column chain cannot express. Method
// bodies never constrain initialization order, so these need no cycle
// handling.
func methodForeignKeys(m *analyze.Model) []*analyze.Field {
	var out []*analyze.Field
	for _, f := range relationFields(m) {
		r := f.Relation
		if r.Kind == analyze.RelBelongsTo && (len(r.Fields) > 1 || r.FKName != "") {
			out = append(out, f)
		}
	}
	return out
}

// inlineForeignKey reports whether a relation renders on its foreign
// key column rather than in a ForeignKeys method: exactly when it is a
// single column belongs to with no constraint name, the only shape the
// column builder chain can express.
func inlineForeignKey(r *analyze.Relation) bool {
	return r.Kind == analyze.RelBelongsTo && len(r.Fields) == 1 && r.FKName == ""
}

// inlineFor finds the relation field whose foreign key rides on the
// given column, returning both because the caller needs the field to
// ask whether this edge was weakened.
func inlineFor(m *analyze.Model, col *analyze.Field) *analyze.Field {
	for _, f := range relationFields(m) {
		if r := f.Relation; inlineForeignKey(r) && r.Fields[0] == col {
			return f
		}
	}
	return nil
}

func (g *generator) printForeignKeys(b *strings.Builder, m *analyze.Model) {
	fks := methodForeignKeys(m)
	if len(fks) == 0 {
		return
	}
	b.WriteString("// ForeignKeys implements orm.ForeignKeyer.\n")
	fmt.Fprintf(b, "func (m *%sModel) ForeignKeys() []orm.ForeignKeyDef {\n", m.Name)
	b.WriteString("\treturn []orm.ForeignKeyDef{\n")
	for _, f := range fks {
		r := f.Relation
		own := make([]string, len(r.Fields))
		for i, k := range r.Fields {
			own[i] = "m." + k.GoName
		}
		far := make([]string, len(r.References))
		for i, k := range r.References {
			if r.Target == m {
				far[i] = "m." + k.GoName
			} else {
				far[i] = varName(r.Target) + "." + k.GoName
			}
		}
		entry := "orm.NewForeignKeyDef(" + strings.Join(own, ", ") + ").References(" + strings.Join(far, ", ") + ")"
		if a := actionExpr(r.OnDelete); a != "" {
			entry += ".OnDelete(" + a + ")"
		}
		if a := actionExpr(r.OnUpdate); a != "" {
			entry += ".OnUpdate(" + a + ")"
		}
		if r.FKName != "" {
			entry += ".Named(" + strconv.Quote(r.FKName) + ")"
		}
		b.WriteString("\t\t" + entry + ",\n")
	}
	b.WriteString("\t}\n}\n\n")
}

func (g *generator) printRelations(b *strings.Builder, m *analyze.Model) {
	rels := relationFields(m)
	if len(rels) == 0 {
		return
	}
	b.WriteString("// Relations implements orm.Relater, naming every key explicitly\n")
	b.WriteString("// so nothing is left to inference.\n")
	fmt.Fprintf(b, "func (m *%sModel) Relations() []orm.RelationDef {\n", m.Name)
	b.WriteString("\treturn []orm.RelationDef{\n")
	for _, f := range rels {
		r := f.Relation
		var entry string
		switch r.Kind {
		case analyze.RelBelongsTo:
			entry = fmt.Sprintf("orm.Via(&m.%s, m.%s)", f.GoName, r.Fields[0].GoName)
		case analyze.RelHasOne, analyze.RelHasMany:
			entry = fmt.Sprintf("orm.Via(&m.%s, %s.%s)", f.GoName, varName(r.Target), r.Fields[0].GoName)
		default:
			entry = fmt.Sprintf("orm.Through(&m.%s, %s.%s, %s.%s)",
				f.GoName,
				varName(r.Through), r.ThroughLocal.GoName,
				varName(r.Through), r.ThroughForeign.GoName)
		}
		b.WriteString("\t\t" + entry + ",\n")
	}
	b.WriteString("\t}\n}\n\n")
}

// hoistedColumns lists the columns a self relation references: they
// must become locals inside the builder closure, because the package
// variable being declared cannot appear in its own initializer.
func hoistedColumns(m *analyze.Model) []*analyze.Field {
	var out []*analyze.Field
	seen := map[*analyze.Field]bool{}
	for _, f := range relationFields(m) {
		r := f.Relation
		if !inlineForeignKey(r) || r.Target != m {
			continue
		}
		ref := r.References[0]
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}

// localName names a hoisted column's local variable after its schema
// field, stepping aside only for the builder parameter t.
func localName(f *analyze.Field) string {
	if f.Name == "t" {
		return "t_"
	}
	return f.Name
}

func (g *generator) printDefineTable(b *strings.Builder, imp *imports, m *analyze.Model) {
	v := varName(m)
	fmt.Fprintf(b, "// %s is the %s table.\n", v, m.TableName)
	fmt.Fprintf(b, "var %s = orm.DefineTable[%s](%q, func(t *orm.TableBuilder[%s]) *%sModel {\n",
		v, m.Name, m.TableName, m.Name, m.Name)

	hoisted := hoistedColumns(m)
	hoistedName := map[*analyze.Field]string{}
	for _, col := range hoisted {
		name := localName(col)
		hoistedName[col] = name
		fmt.Fprintf(b, "\t%s := %s\n", name, g.builderChain(imp, m, col, hoistedName))
	}

	fmt.Fprintf(b, "\treturn &%sModel{\n", m.Name)
	b.WriteString("\t\tTable: t.Table(),\n")
	for _, f := range columnFields(m) {
		if name, ok := hoistedName[f]; ok {
			fmt.Fprintf(b, "\t\t%s: %s,\n", f.GoName, name)
			continue
		}
		fmt.Fprintf(b, "\t\t%s: %s,\n", f.GoName, g.builderChain(imp, m, f, hoistedName))
	}
	b.WriteString("\t}\n})\n")
}

// builderChain renders a column's full builder expression, every
// option in one canonical order so the same schema always prints the
// same bytes.
func (g *generator) builderChain(imp *imports, m *analyze.Model, f *analyze.Field, hoisted map[*analyze.Field]string) string {
	chain := g.constructor(imp, f)
	if f.IsID {
		chain += ".PrimaryKey()"
	}
	if f.Unique {
		chain += ".Unique()"
	}
	if !f.Optional && !f.IsID {
		chain += ".NotNull()"
	}
	if f.Indexed {
		chain += ".Index()"
	}
	if f.VarcharLen > 0 {
		chain += fmt.Sprintf(".MaxLen(%d)", f.VarcharLen)
	}
	if f.NumericPrecision > 0 {
		chain += fmt.Sprintf(".Numeric(%d, %d)", f.NumericPrecision, f.NumericScale)
	}
	if f.JSONText {
		chain += ".JSON()"
	}
	if f.SoftDelete {
		chain += ".SoftDelete()"
	}
	if d := f.Default; d != nil {
		switch d.Kind {
		case analyze.DefaultAutoincrement:
			// Nothing: the ORM derives identity from the key shape.
		case analyze.DefaultUUID, analyze.DefaultGoFunc:
			chain += ".GeneratedByClient(" + g.qualify(imp, d.GoFunc) + ")"
		default:
			chain += ".ServerDefault(" + strconv.Quote(d.SQL) + ")"
		}
	}
	if owner := inlineFor(m, f); owner != nil {
		r := owner.Relation
		ref := r.References[0]
		switch {
		case r.Target == m:
			chain += ".References(" + hoisted[ref] + ")"
		case g.weakened[owner] || ref.Optional:
			// A nullable target column has no typed Ref form of the
			// key's own type, and a cycle edge must not tie
			// initialization order; both drop to naming the table
			// and column, which the ORM resolves at migration time.
			chain += fmt.Sprintf(".ReferencesTable(%q, %q)", r.Target.TableName, ref.ColumnName)
		default:
			chain += ".References(" + varName(r.Target) + "." + ref.GoName + ")"
		}
		if a := actionExpr(r.OnDelete); a != "" {
			chain += ".OnDelete(" + a + ")"
		}
		if a := actionExpr(r.OnUpdate); a != "" {
			chain += ".OnUpdate(" + a + ")"
		}
	}
	return chain
}
