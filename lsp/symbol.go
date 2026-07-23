package lsp

import (
	"github.com/tork-go/orm/gen/analyze"
	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/token"
)

// Hover and go to definition ask the same question first: what is
// under the cursor? Answering it once, against the syntax tree rather
// than the text, is what lets both features agree about where a
// symbol starts and ends.

// symbolKind names what the cursor landed on.
type symbolKind int

const (
	symNone          symbolKind = iota
	symModelName                // the name in "model User {"
	symEnumName                 // the name in "enum Status {"
	symFieldName                // a field's own name
	symTypeRef                  // a type reference in a field
	symFieldRef                 // a field named inside [ ... ] or through:
	symFieldAttrName            // an @ attribute's name
	symBlockAttrName            // a @@ attribute's name
	symEnumValue                // a value inside an enum block
)

// symbol is what the cursor is on, with the span to underline and,
// where it makes sense, the semantic node it refers to.
type symbol struct {
	kind  symbolKind
	span  token.Span
	name  string
	model *analyze.Model // the model the cursor is inside, if any
	field *analyze.Field // the field referred to or declared
	enum  *analyze.Enum  // the enum referred to
	// target is the model a type reference or through: names.
	target *analyze.Model
}

func within(s token.Span, offset int) bool {
	return s.Start.Offset <= offset && offset <= s.End.Offset
}

// symbolAt walks the file's syntax to the smallest construct covering
// the offset. The walk is a plain descent rather than a generic
// visitor: there are six node shapes worth stopping at, and naming
// them here keeps the rules for each visible in one place.
func (f *folder) symbolAt(name string, offset int) symbol {
	for _, d := range f.files[name].Decls {
		switch d := d.(type) {
		case *ast.EnumDecl:
			if !within(d.Span, offset) {
				continue
			}
			e := f.enumNamed(d.Name.Name)
			if within(d.Name.Span, offset) {
				return symbol{kind: symEnumName, span: d.Name.Span, name: d.Name.Name, enum: e}
			}
			for _, v := range d.Values {
				if within(v.Name.Span, offset) {
					return symbol{kind: symEnumValue, span: v.Name.Span, name: v.Name.Name, enum: e}
				}
			}
			for _, a := range d.Attrs {
				if within(a.Name.Span, offset) {
					return symbol{kind: symBlockAttrName, span: a.Name.Span, name: a.Name.Name}
				}
			}
			return symbol{}

		case *ast.ModelDecl:
			if !within(d.Span, offset) {
				continue
			}
			return f.symbolInModel(name, d, offset)
		}
	}
	return symbol{}
}

func (f *folder) symbolInModel(file string, d *ast.ModelDecl, offset int) symbol {
	m := f.modelNamed(d.Name.Name, file)
	if within(d.Name.Span, offset) {
		return symbol{kind: symModelName, span: d.Name.Span, name: d.Name.Name, model: m}
	}
	for _, fd := range d.Fields {
		if !within(fd.Span, offset) {
			continue
		}
		if within(fd.Name.Span, offset) {
			return symbol{
				kind: symFieldName, span: fd.Name.Span, name: fd.Name.Name,
				model: m, field: fieldOf(m, fd.Name.Name),
			}
		}
		if within(fd.Type.Name.Span, offset) {
			return f.typeSymbol(fd.Type.Name, m)
		}
		for _, a := range fd.Attrs {
			if s, ok := f.symbolInAttr(a.Parts, a.Args, m, offset); ok {
				return s
			}
		}
		return symbol{model: m}
	}
	for _, a := range d.Attrs {
		if !within(a.Span, offset) {
			continue
		}
		if within(a.Name.Span, offset) {
			return symbol{kind: symBlockAttrName, span: a.Name.Span, name: a.Name.Name, model: m}
		}
		if s, ok := f.symbolInArgs(a.Args, m, offset); ok {
			return s
		}
	}
	return symbol{model: m}
}

func (f *folder) symbolInAttr(parts []ast.Ident, args []*ast.AttrArg, m *analyze.Model, offset int) (symbol, bool) {
	for _, p := range parts {
		if within(p.Span, offset) {
			return symbol{kind: symFieldAttrName, span: p.Span, name: attrPath(parts), model: m}, true
		}
	}
	return f.symbolInArgs(args, m, offset)
}

// symbolInArgs finds a bare identifier inside an argument list. Those
// are always names of something: a field in a bracketed list, or a
// model after through:, both worth navigating to.
func (f *folder) symbolInArgs(args []*ast.AttrArg, m *analyze.Model, offset int) (symbol, bool) {
	for _, arg := range args {
		named := arg.Name != nil && arg.Name.Name == "through"
		if s, ok := f.symbolInExpr(arg.Value, m, offset, named); ok {
			return s, true
		}
	}
	return symbol{}, false
}

// symbolInExpr descends only into the subtree holding the offset,
// which is both cheaper and the reason each variant below can assume
// the cursor is inside it.
func (f *folder) symbolInExpr(e ast.Expr, m *analyze.Model, offset int, isThrough bool) (symbol, bool) {
	if !within(ast.SpanOf(e), offset) {
		return symbol{}, false
	}
	switch e := e.(type) {
	case *ast.Ident:
		if isThrough {
			return f.typeSymbol(*e, m), true
		}
		return symbol{
			kind: symFieldRef, span: e.Span, name: e.Name,
			model: m, field: fieldOf(m, e.Name),
		}, true
	case *ast.ArrayExpr:
		for _, el := range e.Elems {
			if s, ok := f.symbolInExpr(el, m, offset, false); ok {
				return s, true
			}
		}
	case *ast.FuncCall:
		for _, a := range e.Args {
			if s, ok := f.symbolInExpr(a, m, offset, false); ok {
				return s, true
			}
		}
	}
	return symbol{}, false
}

// typeSymbol resolves a type name to the model or enum it refers to,
// leaving both nil for a built in scalar, which is still worth a hover
// even though it has nowhere to jump to.
func (f *folder) typeSymbol(id ast.Ident, m *analyze.Model) symbol {
	s := symbol{kind: symTypeRef, span: id.Span, name: id.Name, model: m}
	for _, e := range f.schema.Enums {
		if e.Name == id.Name {
			s.enum = e
			return s
		}
	}
	for _, target := range f.schema.Models {
		if target.Name == id.Name {
			s.target = target
			return s
		}
	}
	return s
}

func (f *folder) enumNamed(name string) *analyze.Enum {
	for _, e := range f.schema.Enums {
		if e.Name == name {
			return e
		}
	}
	return nil
}

// modelNamed resolves a model by name, insisting on the declaring file
// too so that a redeclared name resolves to the one actually written
// here rather than whichever the analyzer kept.
func (f *folder) modelNamed(name, file string) *analyze.Model {
	for _, m := range f.schema.Models {
		if m.Name == name && m.File == file {
			return m
		}
	}
	return nil
}

func fieldOf(m *analyze.Model, name string) *analyze.Field {
	if m == nil {
		return nil
	}
	return m.FieldNamed(name)
}

func attrPath(parts []ast.Ident) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "."
		}
		out += p.Name
	}
	return out
}
