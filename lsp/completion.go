package lsp

import (
	"strconv"
	"strings"

	"github.com/tork-go/orm/gen/analyze"
)

// completion suggests what belongs at the cursor. The list is not
// filtered by the partial word: clients filter as the user types, and
// filtering here as well only produces an empty list the moment the
// server's idea of the prefix and the client's differ.
func (s *server) completion(f *folder, name string, pos Position) []CompletionItem {
	doc := f.docs[name]
	ctx := f.contextAt(name, doc.offset(pos))

	switch ctx.kind {
	case ctxTopLevel:
		return items(keywordEntries, kindKeyword)
	case ctxFieldType:
		return f.typeItems()
	case ctxFieldAttr:
		return items(fieldAttrEntries, kindProperty)
	case ctxNativeType:
		return items(nativeEntries, kindProperty)
	case ctxBlockAttr:
		return items(blockAttrEntries, kindProperty)
	case ctxRelationArg:
		return items(relationArgEntries, kindProperty)
	case ctxAction:
		return items(actionEntries, kindEnum)
	case ctxProvider:
		return items(providerEntries, kindEnum)
	case ctxFieldName:
		return fieldItems(ctx.model)
	default:
		return []CompletionItem{}
	}
}

func items(entries []entry, kind int) []CompletionItem {
	out := make([]CompletionItem, 0, len(entries))
	for _, e := range entries {
		out = append(out, CompletionItem{
			Label:         e.label,
			Kind:          kind,
			Detail:        e.detail,
			Documentation: e.doc,
		})
	}
	return out
}

// typeItems offers the built in scalars plus every model and enum the
// schema declares, wherever they were declared. Cross file suggestions
// are the whole reason the server analyzes the folder rather than the
// open file.
func (f *folder) typeItems() []CompletionItem {
	out := items(scalarEntries, kindClass)
	for _, e := range f.schema.Enums {
		out = append(out, CompletionItem{
			Label:         e.Name,
			Kind:          kindEnum,
			Detail:        "enum " + e.DBName,
			Documentation: describeEnum(e),
		})
	}
	for _, m := range f.schema.Models {
		out = append(out, CompletionItem{
			Label:         m.Name,
			Kind:          kindClass,
			Detail:        "model, table " + m.TableName,
			Documentation: describeModel(m),
		})
	}
	return out
}

// fieldItems offers a model's own column fields, which is what belongs
// inside the brackets of a key, index, or relation attribute.
func fieldItems(m *analyze.Model) []CompletionItem {
	if m == nil {
		return []CompletionItem{}
	}
	var out []CompletionItem
	for _, fld := range m.Fields {
		// Keyed on the type rather than on a resolved relation: a
		// model typed field is never a column, even when its relation
		// is too broken to have paired up yet.
		if fld.Type.Kind == analyze.TypeModel {
			continue
		}
		out = append(out, CompletionItem{
			Label:         fld.Name,
			Kind:          kindField,
			Detail:        fld.ColumnName + " " + typeLabel(fld),
			Documentation: fld.Doc,
		})
	}
	if out == nil {
		return []CompletionItem{}
	}
	return out
}

// typeLabel spells a field's type the way the schema does, so a
// suggestion or hover reads back the user's own vocabulary.
func typeLabel(f *analyze.Field) string {
	var name string
	switch f.Type.Kind {
	case analyze.TypeEnum:
		name = f.Type.Enum.Name
	case analyze.TypeModel:
		name = f.Type.Model.Name
	default:
		name = f.Type.Kind.String()
	}
	if f.List {
		name += "[]"
	}
	if f.Optional {
		name += "?"
	}
	return name
}

func describeModel(m *analyze.Model) string {
	var b strings.Builder
	if m.Doc != "" {
		b.WriteString(m.Doc)
		b.WriteString("\n\n")
	}
	b.WriteString("Table ")
	b.WriteString(m.TableName)
	b.WriteString(", ")
	b.WriteString(count(len(m.Fields), "field"))
	if len(m.PrimaryKey) > 0 {
		b.WriteString(", key ")
		names := make([]string, len(m.PrimaryKey))
		for i, k := range m.PrimaryKey {
			names[i] = k.ColumnName
		}
		b.WriteString(strings.Join(names, ", "))
	}
	return b.String()
}

func describeEnum(e *analyze.Enum) string {
	var b strings.Builder
	if e.Doc != "" {
		b.WriteString(e.Doc)
		b.WriteString("\n\n")
	}
	b.WriteString("Database type ")
	b.WriteString(e.DBName)
	b.WriteString(": ")
	b.WriteString(strings.Join(e.Values, ", "))
	return b.String()
}

func count(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
