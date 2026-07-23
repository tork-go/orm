package analyze

import (
	"fmt"
	"sort"

	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/diag"
	"github.com/tork-go/orm/gen/token"
)

// Analyze merges parsed files into one validated Schema. The files are
// one schema directory's worth of .tork sources in any order; models
// and enums live in a single namespace across all of them, which is
// what makes splitting a hundred model application over many files a
// layout choice instead of a semantic one.
//
// The returned diagnostics are sorted by file and position. The Schema
// is always returned, even with errors, because the language server
// completes and hovers against whatever half of the schema is valid;
// the generator, by contrast, must refuse to generate when
// diag.HasErrors reports true.
func Analyze(files []*ast.File) (*Schema, []diag.Diagnostic) {
	a := &analyzer{
		schema: &Schema{},
		models: map[string]*Model{},
		enums:  map[string]*Enum{},
	}
	a.collect(files)
	a.checkDatasource(files)
	for _, e := range a.enumOrder {
		a.fillEnum(e)
	}
	for _, m := range a.modelOrder {
		a.fillModel(m)
	}
	a.resolveRelations()
	a.checkNameCollisions()
	a.finish()
	diag.Sort(a.diags)
	return a.schema, a.diags
}

// analyzer carries the working state of one Analyze call. The order
// slices remember declaration order across files: processing follows
// it so diagnostics come out in the order a reader meets the schema,
// while the finished Schema is sorted by name for determinism.
type analyzer struct {
	diags  []diag.Diagnostic
	schema *Schema

	models     map[string]*Model
	enums      map[string]*Enum
	modelOrder []*Model
	enumOrder  []*Enum

	// pending accumulates relation fields during the model pass; they
	// resolve after every model's columns exist, since fields: and
	// references: may name fields declared later or in other files.
	pending []*pendingRelation

	hasDatasource bool
}

func (a *analyzer) errorf(file string, span token.Span, format string, args ...any) {
	a.diags = append(a.diags, diag.Errorf(file, span, format, args...))
}

func (a *analyzer) warningf(file string, span token.Span, format string, args ...any) {
	a.diags = append(a.diags, diag.Warningf(file, span, format, args...))
}

// at renders a cross reference to where something was first declared,
// in the same file:line:col shape diagnostics themselves use, so the
// reader can jump to both ends of a conflict.
func at(file string, span token.Span) string {
	return fmt.Sprintf("%s:%s", file, span.Start)
}

// scalarTypes maps the DSL's built in type names to their kinds. Enum
// and model names resolve after these, so a model can never shadow a
// built in; collect rejects the attempt outright.
var scalarTypes = map[string]TypeKind{
	"Boolean":  TypeBoolean,
	"Int":      TypeInt,
	"Int32":    TypeInt32,
	"BigInt":   TypeBigInt,
	"Float":    TypeFloat,
	"Double":   TypeDouble,
	"Decimal":  TypeDecimal,
	"String":   TypeString,
	"DateTime": TypeDateTime,
	"Uuid":     TypeUuid,
	"Json":     TypeJson,
}

// collect registers every model and enum name before anything is
// analyzed in depth, because a field's type may live in a file that
// comes later in the read order.
func (a *analyzer) collect(files []*ast.File) {
	for _, f := range files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.ModelDecl:
				a.declareModel(f.Name, d)
			case *ast.EnumDecl:
				a.declareEnum(f.Name, d)
			}
		}
	}
}

func (a *analyzer) declareModel(file string, d *ast.ModelDecl) {
	name := d.Name.Name
	if !isUpperInitial(name) {
		a.errorf(file, d.Name.Span, "model name %q must start with an uppercase letter", name)
	}
	if _, ok := scalarTypes[name]; ok {
		a.errorf(file, d.Name.Span, "%q is a built in type name and cannot be used as a model name", name)
		return
	}
	if prev, ok := a.models[name]; ok {
		a.errorf(file, d.Name.Span, "model %q redeclared (first declared at %s)", name, at(prev.File, prev.Decl.Name.Span))
		return
	}
	if prev, ok := a.enums[name]; ok {
		a.errorf(file, d.Name.Span, "model %q conflicts with the enum of the same name (declared at %s)", name, at(prev.File, prev.Decl.Name.Span))
		return
	}
	m := &Model{Name: name, File: file, Decl: d}
	a.models[name] = m
	a.modelOrder = append(a.modelOrder, m)
}

func (a *analyzer) declareEnum(file string, d *ast.EnumDecl) {
	name := d.Name.Name
	if !isUpperInitial(name) {
		a.errorf(file, d.Name.Span, "enum name %q must start with an uppercase letter", name)
	}
	if _, ok := scalarTypes[name]; ok {
		a.errorf(file, d.Name.Span, "%q is a built in type name and cannot be used as an enum name", name)
		return
	}
	if prev, ok := a.enums[name]; ok {
		a.errorf(file, d.Name.Span, "enum %q redeclared (first declared at %s)", name, at(prev.File, prev.Decl.Name.Span))
		return
	}
	if prev, ok := a.models[name]; ok {
		a.errorf(file, d.Name.Span, "enum %q conflicts with the model of the same name (declared at %s)", name, at(prev.File, prev.Decl.Name.Span))
		return
	}
	e := &Enum{Name: name, DBName: snakeCase(name), File: file, Decl: d}
	a.enums[name] = e
	a.enumOrder = append(a.enumOrder, e)
}

func isUpperInitial(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

func isLowerInitial(name string) bool {
	return name != "" && name[0] >= 'a' && name[0] <= 'z'
}

// checkDatasource enforces exactly one datasource block per schema and
// reads its provider. With no files at all there is nothing to pin the
// diagnostic to, and the caller is about to report the empty directory
// anyway, so that case stays silent.
func (a *analyzer) checkDatasource(files []*ast.File) {
	var found []*ast.DatasourceDecl
	var foundFiles []string
	for _, f := range files {
		for _, d := range f.Decls {
			if ds, ok := d.(*ast.DatasourceDecl); ok {
				found = append(found, ds)
				foundFiles = append(foundFiles, f.Name)
			}
		}
	}
	if len(found) == 0 {
		if len(files) > 0 {
			a.errorf(files[0].Name, token.Span{Start: token.Pos{Offset: 0, Line: 1, Col: 1}, End: token.Pos{Offset: 0, Line: 1, Col: 1}},
				`missing datasource block; add: datasource db { provider = "postgres" }`)
		}
		return
	}
	first, firstFile := found[0], foundFiles[0]
	for i, extra := range found[1:] {
		a.errorf(foundFiles[i+1], extra.Name.Span, "duplicate datasource block (first declared at %s)", at(firstFile, first.Name.Span))
	}

	ds := Datasource{Name: first.Name.Name, File: firstFile, Span: first.Name.Span}
	seen := map[string]bool{}
	for _, entry := range first.Entries {
		if seen[entry.Key.Name] {
			a.errorf(firstFile, entry.Key.Span, "datasource setting %q repeated", entry.Key.Name)
			continue
		}
		seen[entry.Key.Name] = true
		switch entry.Key.Name {
		case "provider":
			lit, ok := entry.Value.(*ast.StringLit)
			if !ok {
				if _, bad := entry.Value.(*ast.BadExpr); !bad {
					a.errorf(firstFile, entry.Span, `provider must be a string, e.g. provider = "postgres"`)
				}
				continue
			}
			if lit.Value != "postgres" {
				a.errorf(firstFile, lit.Span, `unknown provider %q (supported: "postgres")%s`, lit.Value, suggestion(lit.Value, []string{"postgres"}))
				continue
			}
			ds.Provider = lit.Value
		default:
			a.errorf(firstFile, entry.Key.Span, `unknown datasource setting %q (only "provider" is supported)`, entry.Key.Name)
		}
	}
	if !seen["provider"] {
		a.errorf(firstFile, first.Name.Span, `datasource %q is missing its provider (add: provider = "postgres")`, first.Name.Name)
	}
	a.schema.Datasource = ds
	a.hasDatasource = true
}

// checkNameCollisions runs after every model and enum is filled and
// catches the conflicts only visible across declarations: two models
// landing on one table (typically via the pluralizer or @@map), two
// enums landing on one type name, or an enum type name colliding with
// a table, which Postgres rejects because a table already owns a
// composite type of its own name.
func (a *analyzer) checkNameCollisions() {
	tables := map[string]*Model{}
	for _, m := range a.modelOrder {
		if prev, ok := tables[m.TableName]; ok {
			a.errorf(m.File, m.Decl.Name.Span, "table %q is used by both model %q and model %q (rename one with @@map)", m.TableName, prev.Name, m.Name)
			continue
		}
		tables[m.TableName] = m
	}
	types := map[string]*Enum{}
	for _, e := range a.enumOrder {
		if prev, ok := types[e.DBName]; ok {
			a.errorf(e.File, e.Decl.Name.Span, "enum type %q is used by both enum %q and enum %q (rename one with @@map)", e.DBName, prev.Name, e.Name)
			continue
		}
		types[e.DBName] = e
		if m, ok := tables[e.DBName]; ok {
			a.errorf(e.File, e.Decl.Name.Span, "enum type %q collides with the table of model %q; Postgres cannot hold both (rename one with @@map)", e.DBName, m.Name)
		}
	}
}

// finish sorts the finished models and enums into the Schema. Name
// order, not declaration order, so reorganizing files never changes
// generated output.
func (a *analyzer) finish() {
	a.schema.Models = append(a.schema.Models, a.modelOrder...)
	sort.Slice(a.schema.Models, func(i, j int) bool { return a.schema.Models[i].Name < a.schema.Models[j].Name })
	a.schema.Enums = append(a.schema.Enums, a.enumOrder...)
	sort.Slice(a.schema.Enums, func(i, j int) bool { return a.schema.Enums[i].Name < a.schema.Enums[j].Name })
}
