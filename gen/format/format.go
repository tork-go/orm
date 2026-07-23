package format

import (
	"sort"
	"strconv"
	"strings"

	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/diag"
	"github.com/tork-go/orm/gen/parser"
	"github.com/tork-go/orm/gen/token"
)

// Source formats one .tork file. Malformed source is returned exactly
// as it came in, along with the parser's diagnostics: a file the parser
// could only partly understand would lose whatever it could not parse
// if it were reprinted, and silently deleting a user's work is a far
// worse outcome than leaving it unformatted.
func Source(file string, src []byte) ([]byte, []diag.Diagnostic) {
	f, diags := parser.Parse(file, src)
	if diag.HasErrors(diags) {
		return src, diags
	}
	return File(f), diags
}

// File prints a parsed file as canonical source. It assumes the tree
// came from source the parser accepted without errors; Source is the
// entry point that enforces that.
func File(f *ast.File) []byte {
	p := &printer{}
	items := fileItems(f)
	for i, it := range items {
		if i > 0 {
			p.blank()
		}
		p.decl(it)
	}
	return []byte(p.b.String())
}

// printer accumulates output. Blank line handling is deferred rather
// than written eagerly, so a blank before something that turns out to
// be the end of a block never reaches the output.
type printer struct {
	b       strings.Builder
	pending bool
}

func (p *printer) blank() {
	if p.b.Len() > 0 {
		p.pending = true
	}
}

func (p *printer) line(indent int, text string) {
	if p.pending {
		p.b.WriteString("\n")
		p.pending = false
	}
	for range indent {
		p.b.WriteString("\t")
	}
	p.b.WriteString(text)
	p.b.WriteString("\n")
}

// item is one thing printed inside a block, in source order: a member
// or a floating comment. Merging the two into one ordered list is what
// keeps a comment written between two fields between those two fields.
type item struct {
	offset  int
	comment *ast.Comment
	node    any
	// blankBefore records that the author left at least one empty line
	// above this item.
	blankBefore bool
}

// declStart is the offset a declaration effectively begins at: its doc
// comment when it has one, since that is what the reader sees first.
func declStart(doc *ast.CommentGroup, span token.Span) (int, int) {
	if doc != nil && len(doc.Comments) > 0 {
		first := doc.Comments[0].Span
		return first.Start.Offset, first.Start.Line
	}
	return span.Start.Offset, span.Start.Line
}

// order sorts items by source position and marks the ones the author
// separated with a blank line, which is the only spacing information
// worth carrying over from the original.
func order(items []item, endLines map[int]int, startLines map[int]int) []item {
	sort.Slice(items, func(i, j int) bool { return items[i].offset < items[j].offset })
	for i := 1; i < len(items); i++ {
		prevEnd := endLines[items[i-1].offset]
		if startLines[items[i].offset] > prevEnd+1 {
			items[i].blankBefore = true
		}
	}
	return items
}

func fileItems(f *ast.File) []item {
	var items []item
	ends := map[int]int{}
	starts := map[int]int{}
	add := func(offset, startLine, endLine int, it item) {
		it.offset = offset
		items = append(items, it)
		starts[offset] = startLine
		ends[offset] = endLine
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.ModelDecl:
			off, line := declStart(d.Doc, d.Span)
			add(off, line, d.Span.End.Line, item{node: d})
		case *ast.EnumDecl:
			off, line := declStart(d.Doc, d.Span)
			add(off, line, d.Span.End.Line, item{node: d})
		case *ast.DatasourceDecl:
			off, line := declStart(d.Doc, d.Span)
			add(off, line, d.Span.End.Line, item{node: d})
		}
	}
	for i := range f.Floating {
		c := &f.Floating[i]
		add(c.Span.Start.Offset, c.Span.Start.Line, c.Span.End.Line, item{comment: c})
	}
	return order(items, ends, starts)
}

func (p *printer) decl(it item) {
	if it.comment != nil {
		p.line(0, comment(it.comment))
		return
	}
	switch d := it.node.(type) {
	case *ast.DatasourceDecl:
		p.datasource(d)
	case *ast.EnumDecl:
		p.enum(d)
	case *ast.ModelDecl:
		p.model(d)
	}
}

func (p *printer) doc(indent int, g *ast.CommentGroup) {
	if g == nil {
		return
	}
	for i := range g.Comments {
		p.line(indent, comment(&g.Comments[i]))
	}
}

// comment renders one comment with exactly one space after the
// slashes, the spacing gofmt would use, while an empty comment stays
// bare so a divider line of slashes is not padded.
func comment(c *ast.Comment) string {
	text := strings.TrimRight(c.Text, " \t")
	if strings.TrimSpace(text) == "" {
		return "//"
	}
	return "// " + strings.TrimLeft(text, " \t")
}

func trailing(c *ast.Comment) string {
	if c == nil {
		return ""
	}
	return " " + comment(c)
}

func (p *printer) datasource(d *ast.DatasourceDecl) {
	p.doc(0, d.Doc)
	p.line(0, "datasource "+d.Name.Name+" {")
	width := 0
	for _, e := range d.Entries {
		width = max(width, len(e.Key.Name))
	}
	for _, e := range d.Entries {
		p.line(1, pad(e.Key.Name, width)+" = "+expr(e.Value)+trailing(e.Trailing))
	}
	for i := range d.Floating {
		p.line(1, comment(&d.Floating[i]))
	}
	p.line(0, "}")
}

func (p *printer) enum(d *ast.EnumDecl) {
	p.doc(0, d.Doc)
	p.line(0, "enum "+d.Name.Name+" {")
	items, ends, starts := blockItems(nil, d.Values, d.Attrs, d.Floating)
	p.members(order(items, ends, starts), nil)
	p.line(0, "}")
}

func (p *printer) model(d *ast.ModelDecl) {
	p.doc(0, d.Doc)
	p.line(0, "model "+d.Name.Name+" {")
	items, ends, starts := blockItems(d.Fields, nil, d.Attrs, d.Floating)
	p.members(order(items, ends, starts), fieldWidths(d.Fields))
	p.line(0, "}")
}

// blockItems merges a block's members and floating comments into one
// position ordered list, together with the line bounds order needs to
// spot the author's blank lines.
func blockItems(fields []*ast.FieldDecl, values []*ast.EnumValue, attrs []*ast.BlockAttribute, floating []ast.Comment) ([]item, map[int]int, map[int]int) {
	var items []item
	ends := map[int]int{}
	starts := map[int]int{}
	add := func(offset, startLine, endLine int, it item) {
		it.offset = offset
		items = append(items, it)
		starts[offset] = startLine
		ends[offset] = endLine
	}
	for _, f := range fields {
		off, line := declStart(f.Doc, f.Span)
		add(off, line, f.Span.End.Line, item{node: f})
	}
	for _, v := range values {
		off, line := declStart(v.Doc, v.Span)
		add(off, line, v.Span.End.Line, item{node: v})
	}
	for _, a := range attrs {
		off, line := declStart(a.Doc, a.Span)
		add(off, line, a.Span.End.Line, item{node: a})
	}
	for i := range floating {
		c := &floating[i]
		add(c.Span.Start.Offset, c.Span.Start.Line, c.Span.End.Line, item{comment: c})
	}
	return items, ends, starts
}

// widths holds the column widths a model's fields are padded to, so
// types and attributes line up down the block the way they do in the
// schemas this language is modeled on.
type widths struct {
	name int
	typ  int
}

// fieldWidths measures every field, not only the ones carrying
// attributes, so the attribute column sits clear of the longest type
// in the block and the whole model reads as one table.
func fieldWidths(fields []*ast.FieldDecl) *widths {
	w := &widths{}
	for _, f := range fields {
		w.name = max(w.name, len(f.Name.Name))
		w.typ = max(w.typ, len(f.Type.String()))
	}
	return w
}

func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// members prints a block's body. A block attribute always gets a blank
// line above it when members precede it, which is the one piece of
// spacing the formatter imposes rather than preserves: table level
// attributes read as a footer, not as another field.
func (p *printer) members(items []item, w *widths) {
	for i, it := range items {
		_, isAttr := it.node.(*ast.BlockAttribute)
		if it.blankBefore || (isAttr && i > 0 && !previousIsAttr(items, i)) {
			p.blank()
		}
		switch {
		case it.comment != nil:
			p.line(1, comment(it.comment))
		case isAttr:
			a := it.node.(*ast.BlockAttribute)
			p.doc(1, a.Doc)
			p.line(1, "@@"+a.Name.Name+args(a.Args, a.HasParens)+trailing(a.Trailing))
		default:
			switch n := it.node.(type) {
			case *ast.EnumValue:
				p.doc(1, n.Doc)
				p.line(1, n.Name.Name+trailing(n.Trailing))
			case *ast.FieldDecl:
				p.doc(1, n.Doc)
				p.line(1, field(n, w)+trailing(n.Trailing))
			}
		}
	}
}

func previousIsAttr(items []item, i int) bool {
	_, ok := items[i-1].node.(*ast.BlockAttribute)
	return ok
}

func field(f *ast.FieldDecl, w *widths) string {
	line := pad(f.Name.Name, w.name) + " " + f.Type.String()
	if len(f.Attrs) == 0 {
		return line
	}
	line = pad(f.Name.Name, w.name) + " " + pad(f.Type.String(), w.typ)
	parts := make([]string, len(f.Attrs))
	for i, a := range f.Attrs {
		parts[i] = "@" + a.Name() + args(a.Args, a.HasParens)
	}
	return line + " " + strings.Join(parts, " ")
}

// args renders an attribute's arguments, dropping parentheses that
// hold nothing so @unique() and @unique print the same.
func args(list []*ast.AttrArg, hasParens bool) string {
	if !hasParens || len(list) == 0 {
		return ""
	}
	parts := make([]string, len(list))
	for i, a := range list {
		s := expr(a.Value)
		if a.Name != nil {
			s = a.Name.Name + ": " + s
		}
		parts[i] = s
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func expr(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StringLit:
		return quote(e.Value)
	case *ast.IntLit:
		return e.Text
	case *ast.FloatLit:
		return e.Text
	case *ast.BoolLit:
		return strconv.FormatBool(e.Value)
	case *ast.FuncCall:
		parts := make([]string, len(e.Args))
		for i, a := range e.Args {
			parts[i] = expr(a)
		}
		return e.Name.Name + "(" + strings.Join(parts, ", ") + ")"
	default:
		arr := e.(*ast.ArrayExpr)
		parts := make([]string, len(arr.Elems))
		for i, el := range arr.Elems {
			parts[i] = expr(el)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
}

// quote renders a string literal with exactly the escapes the lexer
// decodes, so printing and reparsing round trips any value.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
