package analyze

import (
	"sort"
	"strings"

	"github.com/tork-go/orm/gen/ast"
)

// fieldAttrNames is the suggestion catalog for field attributes.
var fieldAttrNames = []string{
	"id", "unique", "index", "default", "map", "softDelete", "relation",
	"db.VarChar", "db.Text", "db.Numeric", "db.Json", "db.JsonB", "go.type",
}

// dbNativeNames is the @db namespace for the postgres provider.
var dbNativeNames = []string{"VarChar", "Text", "Numeric", "Json", "JsonB"}

// typeCandidates lists every name a field type could have meant, for
// misspelling suggestions: built ins first, then the schema's own
// enums and models, all sorted so ties resolve the same way every run.
func (a *analyzer) typeCandidates() []string {
	var names []string
	for name := range scalarTypes {
		names = append(names, name)
	}
	for name := range a.enums {
		names = append(names, name)
	}
	for name := range a.models {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// buildField resolves one field line and appends it to the model. A
// field whose type failed to resolve is dropped entirely rather than
// half kept, because every later pass would trip over the hole.
func (a *analyzer) buildField(m *Model, fd *ast.FieldDecl) {
	name := fd.Name.Name
	if fd.Type.Name.Name == "" {
		return
	}
	if !isLowerInitial(name) {
		a.errorf(m.File, fd.Name.Span, "field name %q must start with a lowercase letter", name)
	}
	if prev := m.FieldNamed(name); prev != nil {
		a.errorf(m.File, fd.Name.Span, "field %q redeclared in model %q", name, m.Name)
		return
	}
	f := &Field{
		Name:     name,
		GoName:   goName(name),
		Optional: fd.Type.Optional,
		List:     fd.Type.List,
		Doc:      fd.Doc.Text(),
		Model:    m,
		Decl:     fd,
	}
	if !a.resolveFieldType(m, f, fd) {
		return
	}

	var rel *pendingRelation
	seen := map[string]bool{}
	for _, attr := range fd.Attrs {
		if len(attr.Parts) == 0 {
			continue
		}
		attrName := attr.Name()
		if seen[attrName] {
			a.errorf(m.File, attr.Span, "@%s repeated on field %q", attrName, f.Name)
			continue
		}
		seen[attrName] = true
		switch attrName {
		case "id":
			a.idAttr(m, f, attr)
		case "unique":
			if !a.noArgs(m.File, "@unique", attr.Span, attr.Args) {
				continue
			}
			if f.Type.Kind == TypeModel {
				a.errorf(m.File, attr.Span, "@unique cannot apply to a relation field (mark the foreign key field instead)")
				continue
			}
			f.Unique = true
		case "index":
			if !a.noArgs(m.File, "@index", attr.Span, attr.Args) {
				continue
			}
			if f.Type.Kind == TypeModel {
				a.errorf(m.File, attr.Span, "@index cannot apply to a relation field (index the foreign key field instead)")
				continue
			}
			f.Indexed = true
		case "default":
			a.defaultAttr(m, f, attr)
		case "map":
			if f.Type.Kind == TypeModel {
				a.errorf(m.File, attr.Span, "@map cannot apply to a relation field (a relation has no column)")
				continue
			}
			if name, ok := a.mapName(m.File, "@map", "column_name", attr.Span, attr.Args); ok {
				f.ColumnName = name
			}
		case "softDelete":
			a.softDeleteAttr(m, f, attr)
		case "relation":
			if f.Type.Kind != TypeModel {
				a.errorf(m.File, attr.Span, "@relation applies only to fields whose type is a model")
				continue
			}
			rel = a.parseRelationAttr(m, f, attr)
		case "db.VarChar":
			a.varcharAttr(m, f, attr)
		case "db.Text":
			if !a.dbGate(m, attr) || !a.noArgs(m.File, "@db.Text", attr.Span, attr.Args) {
				continue
			}
			if f.Type.Kind != TypeString {
				a.errorf(m.File, attr.Span, "@db.Text applies only to String fields")
			}
		case "db.Numeric":
			a.numericAttr(m, f, attr)
		case "db.Json", "db.JsonB":
			if !a.dbGate(m, attr) || !a.noArgs(m.File, "@"+attrName, attr.Span, attr.Args) {
				continue
			}
			if f.Type.Kind != TypeJson {
				a.errorf(m.File, attr.Span, "@%s applies only to Json fields", attrName)
				continue
			}
			f.JSONText = attrName == "db.Json"
		case "go.type":
			a.goTypeAttr(m, f, attr)
		default:
			if rest, ok := strings.CutPrefix(attrName, "db."); ok {
				a.errorf(m.File, attr.Span, `unknown native type @db.%s for provider "postgres"%s`, rest, suggestion(rest, dbNativeNames))
				continue
			}
			a.errorf(m.File, attr.Span, "unknown attribute @%s%s", attrName, suggestion(attrName, fieldAttrNames))
		}
	}

	if f.Type.Kind == TypeJson && f.GoType.Name == "" && !seen["go.type"] {
		a.errorf(m.File, fd.Name.Span, `a Json field needs @go.type to name its Go type, e.g. @go.type("Profile")`)
	}
	if f.Type.Kind != TypeModel && f.ColumnName == "" {
		f.ColumnName = snakeCase(name)
	}
	if f.Type.Kind == TypeModel {
		if rel == nil {
			rel = &pendingRelation{field: f}
		}
		a.pending = append(a.pending, rel)
	}
	m.Fields = append(m.Fields, f)
}

// resolveFieldType resolves the type name and checks the shape rules
// that depend only on the type itself.
func (a *analyzer) resolveFieldType(m *Model, f *Field, fd *ast.FieldDecl) bool {
	typeName := fd.Type.Name.Name
	if kind, ok := scalarTypes[typeName]; ok {
		f.Type = FieldType{Kind: kind}
	} else if e := a.enums[typeName]; e != nil {
		f.Type = FieldType{Kind: TypeEnum, Enum: e}
	} else if target := a.models[typeName]; target != nil {
		f.Type = FieldType{Kind: TypeModel, Model: target}
	} else {
		a.errorf(m.File, fd.Type.Name.Span, "unknown type %q%s", typeName, suggestion(typeName, a.typeCandidates()))
		return false
	}
	switch f.Type.Kind {
	case TypeEnum:
		if f.List {
			a.errorf(m.File, fd.Type.Span, "enum fields cannot be lists (the ORM has no enum array column)")
			return false
		}
	case TypeJson:
		if f.List {
			a.errorf(m.File, fd.Type.Span, "Json fields cannot be lists (bind a slice type with @go.type instead)")
			return false
		}
	case TypeModel:
		if f.List && f.Optional {
			a.errorf(m.File, fd.Type.Span, "a relation list cannot be optional (it loads as an empty slice when there are no rows)")
			return false
		}
	}
	return true
}

// idAttr applies @id. The eligibility rules mirror keyIneligibility so
// @id and @@id reject the same fields with the same vocabulary.
func (a *analyzer) idAttr(m *Model, f *Field, attr *ast.Attribute) {
	if !a.noArgs(m.File, "@id", attr.Span, attr.Args) {
		return
	}
	switch keyIneligibility(f) {
	case "":
	case "relation":
		a.errorf(m.File, attr.Span, "@id cannot apply to a relation field (mark the foreign key field instead)")
		return
	case "optional":
		a.errorf(m.File, attr.Span, "@id cannot apply to an optional field")
		return
	case "list":
		a.errorf(m.File, attr.Span, "@id cannot apply to a list field")
		return
	default:
		a.errorf(m.File, attr.Span, "@id cannot apply to a Json field")
		return
	}
	if len(m.PrimaryKey) > 0 {
		a.errorf(m.File, attr.Span, "duplicate @id; model %q already marks %q as its primary key", m.Name, m.PrimaryKey[0].Name)
		return
	}
	f.IsID = true
	m.PrimaryKey = []*Field{f}
}

// softDeleteAttr applies @softDelete, which the ORM restricts to a
// nullable timestamp; the schema enforces the same shape up front.
func (a *analyzer) softDeleteAttr(m *Model, f *Field, attr *ast.Attribute) {
	if !a.noArgs(m.File, "@softDelete", attr.Span, attr.Args) {
		return
	}
	if f.Type.Kind != TypeDateTime || !f.Optional || f.List {
		a.errorf(m.File, attr.Span, "@softDelete requires an optional DateTime field (DateTime?)")
		return
	}
	if m.SoftDelete != nil {
		a.errorf(m.File, attr.Span, "model %q already has a soft delete field (%q)", m.Name, m.SoftDelete.Name)
		return
	}
	f.SoftDelete = true
	m.SoftDelete = f
}

// dbGate refuses @db natives when no datasource names a provider,
// since the namespace is defined by the provider.
func (a *analyzer) dbGate(m *Model, attr *ast.Attribute) bool {
	if !a.hasDatasource {
		a.errorf(m.File, attr.Span, `@db attributes require a datasource block (add: datasource db { provider = "postgres" })`)
		return false
	}
	return true
}

func (a *analyzer) varcharAttr(m *Model, f *Field, attr *ast.Attribute) {
	if !a.dbGate(m, attr) {
		return
	}
	if f.Type.Kind != TypeString {
		a.errorf(m.File, attr.Span, "@db.VarChar applies only to String fields")
		return
	}
	const usage = "@db.VarChar needs a positive length, e.g. @db.VarChar(255)"
	if !a.positional(m.File, "@db.VarChar", attr.Args) {
		return
	}
	if len(attr.Args) != 1 {
		a.errorf(m.File, attr.Span, "%s", usage)
		return
	}
	n, ok := a.intArg(m.File, usage, attr.Args[0].Value)
	if !ok {
		return
	}
	if n <= 0 {
		a.errorf(m.File, spanOf(attr.Args[0].Value), "%s", usage)
		return
	}
	f.VarcharLen = n
}

func (a *analyzer) numericAttr(m *Model, f *Field, attr *ast.Attribute) {
	if !a.dbGate(m, attr) {
		return
	}
	if f.Type.Kind != TypeDecimal {
		a.errorf(m.File, attr.Span, "@db.Numeric applies only to Decimal fields")
		return
	}
	const usage = "@db.Numeric needs precision and scale, e.g. @db.Numeric(10, 2)"
	if !a.positional(m.File, "@db.Numeric", attr.Args) {
		return
	}
	if len(attr.Args) != 2 {
		a.errorf(m.File, attr.Span, "%s", usage)
		return
	}
	precision, ok := a.intArg(m.File, usage, attr.Args[0].Value)
	if !ok {
		return
	}
	scale, ok := a.intArg(m.File, usage, attr.Args[1].Value)
	if !ok {
		return
	}
	if precision <= 0 || scale < 0 {
		a.errorf(m.File, attr.Span, "%s", usage)
		return
	}
	if scale > precision {
		a.errorf(m.File, spanOf(attr.Args[1].Value), "@db.Numeric scale cannot exceed precision")
		return
	}
	f.NumericPrecision = precision
	f.NumericScale = scale
}

func (a *analyzer) goTypeAttr(m *Model, f *Field, attr *ast.Attribute) {
	if f.Type.Kind != TypeJson {
		a.errorf(m.File, attr.Span, "@go.type applies only to Json fields")
		return
	}
	value, span, ok := a.oneString(m.File, "@go.type", `"Profile"`, attr.Span, attr.Args)
	if !ok {
		return
	}
	ref, ok := parseGoRef(value)
	if !ok {
		a.errorf(m.File, span, `invalid Go type reference %q (write "Name" for a type in the generated package, or "import/path.Name")`, value)
		return
	}
	f.GoType = ref
}
