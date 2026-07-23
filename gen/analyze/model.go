package analyze

import (
	"github.com/tork-go/orm/gen/ast"
)

// FieldNamed finds a field by its schema name. Linear scan on purpose:
// models hold tens of fields, and the callers (index resolution, the
// language server) are nowhere near hot.
func (m *Model) FieldNamed(name string) *Field {
	for _, f := range m.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// columnFieldNames lists the model's column field names, the candidate
// set behind "did you mean" suggestions in key and index attributes.
func (m *Model) columnFieldNames() []string {
	var names []string
	for _, f := range m.Fields {
		if f.Type.Kind != TypeModel {
			names = append(names, f.Name)
		}
	}
	return names
}

// blockAttrNames is the suggestion catalog for @@ attributes.
var blockAttrNames = []string{"id", "unique", "index", "check", "map"}

// fillModel resolves one model: fields first, then block attributes,
// then the checks that need the whole picture, such as column
// collisions and the single integer primary key rules.
func (a *analyzer) fillModel(m *Model) {
	m.Doc = m.Decl.Doc.Text()
	m.TableName = pluralize(snakeCase(m.Name))

	for _, fd := range m.Decl.Fields {
		a.buildField(m, fd)
	}
	// The emptiness check reads the syntax, not the survivors: a model
	// whose only field was dropped for a bad type already got its
	// diagnostic, and "has no fields" on top would be an echo.
	if len(m.Decl.Fields) == 0 {
		a.errorf(m.File, m.Decl.Name.Span, "model %q has no fields", m.Name)
	}

	indexNames := map[string]bool{}
	for _, attr := range m.Decl.Attrs {
		switch attr.Name.Name {
		case "map":
			if name, ok := a.mapName(m.File, "@@map", "table_name", attr.Span, attr.Args); ok {
				m.TableName = name
			}
		case "id":
			a.compositeID(m, attr)
		case "unique":
			a.uniqueAttr(m, attr, indexNames)
		case "index":
			a.indexAttr(m, attr, indexNames)
		case "check":
			a.checkAttr(m, attr)
		default:
			a.errorf(m.File, attr.Span, "unknown attribute @@%s%s", attr.Name.Name, suggestion(attr.Name.Name, blockAttrNames))
		}
	}

	a.checkFieldCollisions(m)
	a.checkPrimaryKey(m)
}

// checkFieldCollisions catches two fields landing on one column or one
// Go identifier. Both are legal schemas character by character and
// broken programs afterwards, which is exactly what analysis is for.
func (a *analyzer) checkFieldCollisions(m *Model) {
	columns := map[string]*Field{}
	goNames := map[string]*Field{}
	for _, f := range m.Fields {
		if prev, ok := goNames[f.GoName]; ok {
			a.errorf(m.File, f.Decl.Name.Span, "fields %q and %q produce the same Go field name %q; rename one", prev.Name, f.Name, f.GoName)
		} else {
			goNames[f.GoName] = f
		}
		if f.ColumnName == "" {
			continue
		}
		if prev, ok := columns[f.ColumnName]; ok {
			a.errorf(m.File, f.Decl.Name.Span, "fields %q and %q both map to column %q in model %q (adjust @map)", prev.Name, f.Name, f.ColumnName, m.Name)
			continue
		}
		columns[f.ColumnName] = f
	}
}

// compositeID resolves @@id([a, b]) into the model's primary key.
func (a *analyzer) compositeID(m *Model, attr *ast.BlockAttribute) {
	if len(m.PrimaryKey) > 0 {
		if len(m.PrimaryKey) == 1 && hasIDAttr(m.PrimaryKey[0]) {
			a.errorf(m.File, attr.Span, "model %q has both @id and @@id; use one", m.Name)
		} else {
			a.errorf(m.File, attr.Span, "@@id repeated in model %q", m.Name)
		}
		return
	}
	if !a.positional(m.File, "@@id", attr.Args) {
		return
	}
	if len(attr.Args) != 1 {
		a.errorf(m.File, attr.Span, "@@id needs a non empty field list, e.g. @@id([a, b])")
		return
	}
	idents, ok := a.identList(m.File, "@@id needs a non empty field list, e.g. @@id([a, b])", attr.Args[0].Value)
	if !ok {
		return
	}
	if len(idents) == 0 {
		a.errorf(m.File, attr.Span, "@@id needs a non empty field list, e.g. @@id([a, b])")
		return
	}
	seen := map[string]bool{}
	var fields []*Field
	for _, id := range idents {
		f := m.FieldNamed(id.Name)
		if f == nil {
			a.errorf(m.File, id.Span, "unknown field %q in @@id%s", id.Name, suggestion(id.Name, m.columnFieldNames()))
			return
		}
		if seen[id.Name] {
			a.errorf(m.File, id.Span, "field %q repeated in @@id", id.Name)
			return
		}
		seen[id.Name] = true
		if reason := keyIneligibility(f); reason != "" {
			a.errorf(m.File, id.Span, "@@id cannot include %s field %q", reason, f.Name)
			return
		}
		fields = append(fields, f)
	}
	for _, f := range fields {
		f.IsID = true
	}
	m.PrimaryKey = fields
}

// hasIDAttr reports whether a field's own attributes spell @id, which
// tells "@id plus @@id" apart from "two @@id" when both slip in.
func hasIDAttr(f *Field) bool {
	for _, attr := range f.Decl.Attrs {
		if len(attr.Parts) == 1 && attr.Parts[0].Name == "id" {
			return true
		}
	}
	return false
}

// keyIneligibility names why a field cannot join a primary key, or
// returns empty for an eligible one.
func keyIneligibility(f *Field) string {
	switch {
	case f.Type.Kind == TypeModel:
		return "relation"
	case f.Type.Kind == TypeJson:
		return "Json"
	case f.List:
		return "list"
	case f.Optional:
		return "optional"
	}
	return ""
}

// resolveIndexFields resolves an index attribute's field list against
// the model, shared by @@unique and @@index.
func (a *analyzer) resolveIndexFields(m *Model, label string, idents []ast.Ident) ([]*Field, bool) {
	var fields []*Field
	for _, id := range idents {
		f := m.FieldNamed(id.Name)
		if f == nil {
			a.errorf(m.File, id.Span, "unknown field %q in %s%s", id.Name, label, suggestion(id.Name, m.columnFieldNames()))
			return nil, false
		}
		if f.Type.Kind == TypeModel {
			a.errorf(m.File, id.Span, "%q is a relation field; index its foreign key column instead", id.Name)
			return nil, false
		}
		fields = append(fields, f)
	}
	return fields, true
}

// indexName validates and registers an index's name: argument, keeping
// names unique within the model because they become constraint names
// in one shared namespace.
func (a *analyzer) indexName(m *Model, value string, arg *ast.AttrArg, names map[string]bool) (string, bool) {
	if !isSQLIdent(value) {
		a.errorf(m.File, spanOf(arg.Value), "index name %q is not a valid identifier", value)
		return "", false
	}
	if names[value] {
		a.errorf(m.File, spanOf(arg.Value), "index name %q repeated in model %q", value, m.Name)
		return "", false
	}
	names[value] = true
	return value, true
}

// uniqueAttr resolves @@unique([a, b], name: "..."). It builds the same
// Index the generator renders through NewIndexDef(...).Unique().
func (a *analyzer) uniqueAttr(m *Model, attr *ast.BlockAttribute, names map[string]bool) {
	idx := &Index{Unique: true, Decl: attr}
	var idents []ast.Ident
	for _, arg := range attr.Args {
		switch {
		case arg.Name == nil:
			var ok bool
			idents, ok = a.identList(m.File, "@@unique needs a non empty field list, e.g. @@unique([a, b])", arg.Value)
			if !ok {
				return
			}
		case arg.Name.Name == "name":
			value, ok := a.namedString(m.File, "@@unique", arg)
			if !ok {
				return
			}
			name, ok := a.indexName(m, value, arg, names)
			if !ok {
				return
			}
			idx.Name = name
		default:
			a.errorf(m.File, arg.Name.Span, "unknown argument %q in @@unique%s", arg.Name.Name, suggestion(arg.Name.Name, []string{"name"}))
			return
		}
	}
	if len(idents) == 0 {
		a.errorf(m.File, attr.Span, "@@unique needs a non empty field list, e.g. @@unique([a, b])")
		return
	}
	fields, ok := a.resolveIndexFields(m, "@@unique", idents)
	if !ok {
		return
	}
	idx.Fields = fields
	m.Indexes = append(m.Indexes, idx)
}

// indexAttr resolves @@index([...], name:, where:, on:). The field list
// may be empty only when on: supplies expression keys, mirroring the
// ORM's NewIndexDef()...On(...) form for expression indexes.
func (a *analyzer) indexAttr(m *Model, attr *ast.BlockAttribute, names map[string]bool) {
	idx := &Index{Decl: attr}
	var idents []ast.Ident
	for _, arg := range attr.Args {
		switch {
		case arg.Name == nil:
			var ok bool
			idents, ok = a.identList(m.File, "@@index expects a field list, e.g. @@index([a, b])", arg.Value)
			if !ok {
				return
			}
		case arg.Name.Name == "name":
			value, ok := a.namedString(m.File, "@@index", arg)
			if !ok {
				return
			}
			name, ok := a.indexName(m, value, arg, names)
			if !ok {
				return
			}
			idx.Name = name
		case arg.Name.Name == "where":
			value, ok := a.namedString(m.File, "@@index", arg)
			if !ok {
				return
			}
			if value == "" {
				a.errorf(m.File, spanOf(arg.Value), "where: expects a SQL predicate, e.g. where: \"deleted_at IS NULL\"")
				return
			}
			idx.Where = value
		case arg.Name.Name == "on":
			exprs, ok := a.stringList(m.File, `on: expects non empty SQL expressions, e.g. on: ["lower(email)"]`, arg.Value)
			if !ok {
				return
			}
			idx.Expressions = exprs
		default:
			a.errorf(m.File, arg.Name.Span, "unknown argument %q in @@index%s", arg.Name.Name, suggestion(arg.Name.Name, []string{"name", "where", "on"}))
			return
		}
	}
	if len(idents) == 0 && len(idx.Expressions) == 0 {
		a.errorf(m.File, attr.Span, "@@index needs fields or on: expressions")
		return
	}
	fields, ok := a.resolveIndexFields(m, "@@index", idents)
	if !ok {
		return
	}
	idx.Fields = fields
	m.Indexes = append(m.Indexes, idx)
}

// namedString reads the string value of a named argument like
// name: "idx_users_email".
func (a *analyzer) namedString(file, label string, arg *ast.AttrArg) (string, bool) {
	lit, ok := arg.Value.(*ast.StringLit)
	if !ok {
		if !isBad(arg.Value) {
			a.errorf(file, spanOf(arg.Value), "%s: expects a string in %s", arg.Name.Name, label)
		}
		return "", false
	}
	return lit.Value, true
}

// checkAttr resolves @@check("expr", name: "...").
func (a *analyzer) checkAttr(m *Model, attr *ast.BlockAttribute) {
	check := &Check{Decl: attr}
	for _, arg := range attr.Args {
		switch {
		case arg.Name == nil:
			lit, ok := arg.Value.(*ast.StringLit)
			if !ok {
				if !isBad(arg.Value) {
					a.errorf(m.File, spanOf(arg.Value), `@@check needs a SQL expression, e.g. @@check("pages > 0")`)
				}
				return
			}
			check.Expression = lit.Value
		case arg.Name.Name == "name":
			value, ok := a.namedString(m.File, "@@check", arg)
			if !ok {
				return
			}
			if !isSQLIdent(value) {
				a.errorf(m.File, spanOf(arg.Value), "check name %q is not a valid identifier", value)
				return
			}
			check.Name = value
		default:
			a.errorf(m.File, arg.Name.Span, "unknown argument %q in @@check%s", arg.Name.Name, suggestion(arg.Name.Name, []string{"name"}))
			return
		}
	}
	if check.Expression == "" {
		a.errorf(m.File, attr.Span, `@@check needs a SQL expression, e.g. @@check("pages > 0")`)
		return
	}
	m.Checks = append(m.Checks, check)
}

// checkPrimaryKey applies the rules that depend on the finished key:
// autoincrement placement, and the ORM's derived identity for a single
// integer primary key, which the schema should acknowledge rather than
// silently receive.
func (a *analyzer) checkPrimaryKey(m *Model) {
	soleIntPK := len(m.PrimaryKey) == 1 && isIntegerKind(m.PrimaryKey[0].Type.Kind)
	for _, f := range m.Fields {
		if f.Default == nil || f.Default.Kind != DefaultAutoincrement {
			continue
		}
		switch {
		case !f.IsID:
			a.errorf(m.File, f.Default.Span, "@default(autoincrement()) requires the field to be the primary key")
			f.Default = nil
		case len(m.PrimaryKey) > 1:
			a.errorf(m.File, f.Default.Span, "@default(autoincrement()) requires a single column primary key")
			f.Default = nil
		}
	}
	if !soleIntPK {
		return
	}
	pk := m.PrimaryKey[0]
	switch {
	case pk.Default == nil:
		a.warningf(m.File, pk.Decl.Name.Span, "%q is the single integer primary key, so it becomes GENERATED ALWAYS AS IDENTITY; add @default(autoincrement()) to make that explicit", pk.Name)
	case pk.Default.Kind == DefaultLiteral || pk.Default.Kind == DefaultDBGenerated:
		a.errorf(m.File, pk.Default.Span, "%q is a generated identity column (single integer primary key); it cannot also have a server side default", pk.Name)
		pk.Default = nil
	}
}

func isIntegerKind(k TypeKind) bool {
	return k == TypeInt || k == TypeInt32 || k == TypeBigInt
}
