package lsp

import (
	"github.com/tork-go/orm/gen/analyze"
	"github.com/tork-go/orm/gen/token"
)

// definition answers go to definition. Because the whole folder is
// analyzed, a type declared in another file resolves as readily as one
// declared above the cursor, which is what makes splitting a large
// schema across files free.
func (s *server) definition(f *folder, name string, pos Position) []Location {
	doc := f.docs[name]
	sym := f.symbolAt(name, doc.offset(pos))

	switch sym.kind {
	case symTypeRef:
		switch {
		case sym.enum != nil:
			return f.locate(sym.enum.File, sym.enum.Decl.Name.Span)
		case sym.target != nil:
			return f.locate(sym.target.File, sym.target.Decl.Name.Span)
		}
	case symFieldRef:
		if sym.field != nil {
			return f.locate(sym.field.Model.File, sym.field.Decl.Name.Span)
		}
	case symFieldName:
		// A relation field jumps to its counterpart, which is the
		// other half of what the user is reading and is usually in
		// another file.
		if sym.field != nil && sym.field.Relation != nil && sym.field.Relation.Inverse != nil {
			inverse := sym.field.Relation.Inverse
			return f.locate(inverse.Model.File, inverse.Decl.Name.Span)
		}
	case symModelName:
		return f.modelReferences(sym.model)
	}
	return nil
}

// locate turns a file and span into the single location a client
// expects. Every file the semantic model names was parsed from this
// folder, so the lookup cannot miss.
func (f *folder) locate(file string, span token.Span) []Location {
	return []Location{{URI: f.uriFor(file), Range: f.docs[file].rangeOf(span)}}
}

// modelReferences answers go to definition on a model's own name with
// every relation field pointing at it. Standing on a declaration and
// asking where it is used is the question people actually have there,
// and no other feature answers it.
func (f *folder) modelReferences(m *analyze.Model) []Location {
	if m == nil {
		return nil
	}
	var out []Location
	for _, other := range f.schema.Models {
		for _, fld := range other.Fields {
			if fld.Type.Kind != analyze.TypeModel || fld.Type.Model != m {
				continue
			}
			out = append(out, Location{
				URI:   f.uriFor(other.File),
				Range: f.docs[other.File].rangeOf(fld.Decl.Type.Name.Span),
			})
		}
	}
	return out
}
