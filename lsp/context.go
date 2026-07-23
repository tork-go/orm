package lsp

import (
	"strings"

	"github.com/tork-go/orm/gen/analyze"
)

// Completion has to answer for source that is, by definition, half
// written: the user is mid word, and the tree around the cursor may be
// a Bad node. Rather than hunt through a partial tree, the context is
// read off the text of the line up to the cursor, which is stable no
// matter how badly the rest of the file parses. The analyzed schema
// still supplies the names, so suggestions stay accurate across files.

// completionKind names what belongs at the cursor.
type completionKind int

const (
	// ctxNone is a position nothing sensible can be suggested for.
	ctxNone        completionKind = iota
	ctxTopLevel                   // a declaration keyword
	ctxFieldType                  // a field's type
	ctxFieldAttr                  // an @ attribute name
	ctxNativeType                 // an @db. native type name
	ctxBlockAttr                  // an @@ attribute name
	ctxFieldName                  // a field name inside [ ... ]
	ctxRelationArg                // an argument name inside @relation( ... )
	ctxAction                     // a referential action value
	ctxProvider                   // the datasource provider value
)

// cursorContext is what completion needs to know: the kind of thing
// wanted, the model it is wanted in, and the partial word typed so far.
type cursorContext struct {
	kind   completionKind
	model  *analyze.Model
	prefix string
}

// contextAt reads the line up to the cursor and decides what belongs
// there.
func (f *folder) contextAt(name string, offset int) cursorContext {
	doc := f.docs[name]
	line := lineUpTo(doc, offset)
	model := f.modelIn(name, offset)
	inBlock := insideBlock(doc, offset)

	trimmed := strings.TrimLeft(line, " \t")
	word := trailingWord(line)

	// A provider entry is recognized wherever it appears, before
	// anything else looks at the line, because it is the one place a
	// bare value belongs inside a block that has no fields.
	if strings.Contains(line, "provider") && strings.Contains(line, "=") {
		return cursorContext{kind: ctxProvider, prefix: word}
	}

	switch {
	case strings.HasPrefix(trimmed, "@@"):
		if inner, ok := openArgument(line); ok {
			return f.argumentContext(model, inner, word, blockAttrName(trimmed))
		}
		return cursorContext{kind: ctxBlockAttr, model: model, prefix: strings.TrimPrefix(trimmed, "@@")}

	case !inBlock:
		// Outside any block only a declaration keyword belongs, and
		// only while the first word is still being typed.
		if strings.ContainsAny(trimmed, " \t") {
			return cursorContext{kind: ctxNone}
		}
		return cursorContext{kind: ctxTopLevel, prefix: trimmed}
	}

	if inner, ok := openArgument(line); ok {
		return f.argumentContext(model, inner, word, "")
	}
	if at := strings.LastIndexByte(line, '@'); at >= 0 && !strings.ContainsAny(line[at:], "()") {
		rest := line[at+1:]
		if ns, member, ok := strings.Cut(rest, "."); ok && ns == "db" {
			return cursorContext{kind: ctxNativeType, model: model, prefix: member}
		}
		return cursorContext{kind: ctxFieldAttr, model: model, prefix: rest}
	}
	// A field line with a name typed and a space after it wants a type.
	if fields := strings.Fields(line); len(fields) == 1 && endsWithSpace(line) || len(fields) == 2 && !endsWithSpace(line) {
		return cursorContext{kind: ctxFieldType, model: model, prefix: word}
	}
	return cursorContext{kind: ctxNone, model: model}
}

// argumentContext classifies a position inside an attribute's
// parentheses, where the useful suggestions are field names, argument
// keywords, and referential actions.
func (f *folder) argumentContext(model *analyze.Model, inner, word, blockAttr string) cursorContext {
	if strings.Count(inner, "[") > strings.Count(inner, "]") {
		return cursorContext{kind: ctxFieldName, model: model, prefix: word}
	}
	key := lastArgumentKey(inner)
	switch key {
	case "onDelete", "onUpdate":
		return cursorContext{kind: ctxAction, model: model, prefix: word}
	case "through":
		return cursorContext{kind: ctxFieldType, model: model, prefix: word}
	}
	if strings.Contains(inner, "@relation") || blockAttr == "" && strings.Contains(inner, "fields") {
		return cursorContext{kind: ctxRelationArg, model: model, prefix: word}
	}
	if blockAttr != "" {
		return cursorContext{kind: ctxNone, model: model}
	}
	return cursorContext{kind: ctxRelationArg, model: model, prefix: word}
}

// lineUpTo returns the text of the cursor's line, up to the cursor.
func lineUpTo(doc *document, offset int) string {
	start := strings.LastIndexByte(doc.text[:offset], '\n') + 1
	return doc.text[start:offset]
}

func endsWithSpace(line string) bool {
	return line != "" && (line[len(line)-1] == ' ' || line[len(line)-1] == '\t')
}

// trailingWord is the partial identifier being typed, which the client
// uses to filter the suggestions.
func trailingWord(line string) string {
	i := len(line)
	for i > 0 && isWordByte(line[i-1]) {
		i--
	}
	return line[i:]
}

func isWordByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// blockAttrName reads the attribute word off a @@ line.
func blockAttrName(trimmed string) string {
	rest := strings.TrimPrefix(trimmed, "@@")
	i := 0
	for i < len(rest) && isWordByte(rest[i]) {
		i++
	}
	return rest[:i]
}

// openArgument returns the text inside the innermost unclosed
// parenthesis on the line, and whether the cursor is inside one at all.
func openArgument(line string) (string, bool) {
	depth := 0
	start := -1
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '(':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
	}
	if depth == 0 || start < 0 {
		return "", false
	}
	return line[start:], true
}

// lastArgumentKey is the name of the argument being given a value, as
// in the "onDelete" of "onDelete: Cas".
func lastArgumentKey(inner string) string {
	colon := strings.LastIndexByte(inner, ':')
	if colon < 0 {
		return ""
	}
	if comma := strings.LastIndexByte(inner, ','); comma > colon {
		return ""
	}
	return trailingWord(strings.TrimRight(inner[:colon], " \t"))
}

// insideBlock reports whether an offset sits between an unclosed brace
// and its match. Counting braces in the text rather than consulting
// the tree is deliberate: the block being typed into is usually the
// one that failed to parse, and its node may be missing or Bad, while
// the braces above the cursor are exactly as the user left them.
func insideBlock(doc *document, offset int) bool {
	depth := 0
	for i := 0; i < offset; i++ {
		switch doc.text[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		}
	}
	return depth > 0
}
