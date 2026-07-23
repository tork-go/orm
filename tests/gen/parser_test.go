package gen_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/parser"
	"github.com/tork-go/orm/gen/token"
)

// dumpFile flattens a syntax tree into one line per construct so tests
// can state expectations as readable text instead of nested struct
// literals. The format quotes doc and trailing comments, which makes
// comment attachment visible in the same assertions as structure.
func dumpFile(f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.DatasourceDecl:
			out = append(out, "datasource "+d.Name.Name+suffixes(d.Doc, nil))
			for _, e := range d.Entries {
				out = append(out, "  "+e.Key.Name+" = "+exprString(e.Value)+suffixes(nil, e.Trailing))
			}
			out = append(out, floatLines(d.Floating)...)
		case *ast.EnumDecl:
			out = append(out, "enum "+d.Name.Name+suffixes(d.Doc, nil))
			for _, v := range d.Values {
				out = append(out, "  "+v.Name.Name+suffixes(v.Doc, v.Trailing))
			}
			for _, a := range d.Attrs {
				out = append(out, "  "+blockAttrString(a)+suffixes(a.Doc, a.Trailing))
			}
			out = append(out, floatLines(d.Floating)...)
		case *ast.ModelDecl:
			out = append(out, "model "+d.Name.Name+suffixes(d.Doc, nil))
			for _, fd := range d.Fields {
				typ := fd.Type.String()
				if fd.Type.Name.Name == "" {
					typ = "<missing>"
				}
				line := "  field " + fd.Name.Name + " " + typ
				if len(fd.Attrs) > 0 {
					attrs := make([]string, len(fd.Attrs))
					for i, a := range fd.Attrs {
						attrs[i] = attrString(a)
					}
					line += " attrs=[" + strings.Join(attrs, ", ") + "]"
				}
				out = append(out, line+suffixes(fd.Doc, fd.Trailing))
			}
			for _, a := range d.Attrs {
				out = append(out, "  "+blockAttrString(a)+suffixes(a.Doc, a.Trailing))
			}
			out = append(out, floatLines(d.Floating)...)
		case *ast.BadDecl:
			out = append(out, "bad")
		}
	}
	out = append(out, floatLines(f.Floating)...)
	return out
}

func suffixes(doc *ast.CommentGroup, trail *ast.Comment) string {
	s := ""
	if doc != nil {
		s += fmt.Sprintf(" doc=%q", doc.Text())
	}
	if trail != nil {
		s += fmt.Sprintf(" trail=%q", strings.TrimSpace(trail.Text))
	}
	return s
}

func floatLines(comments []ast.Comment) []string {
	var out []string
	for _, c := range comments {
		out = append(out, fmt.Sprintf("  floating %q", strings.TrimSpace(c.Text)))
	}
	return out
}

func argsString(args []*ast.AttrArg, hasParens bool) string {
	if !hasParens {
		return ""
	}
	parts := make([]string, len(args))
	for i, a := range args {
		s := exprString(a.Value)
		if a.Name != nil {
			s = a.Name.Name + ": " + s
		}
		parts[i] = s
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func attrString(a *ast.Attribute) string {
	return "@" + a.Name() + argsString(a.Args, a.HasParens)
}

func blockAttrString(b *ast.BlockAttribute) string {
	return "@@" + b.Name.Name + argsString(b.Args, b.HasParens)
}

func exprString(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StringLit:
		return fmt.Sprintf("%q", e.Value)
	case *ast.IntLit:
		return e.Text
	case *ast.FloatLit:
		return e.Text
	case *ast.BoolLit:
		return fmt.Sprintf("%t", e.Value)
	case *ast.FuncCall:
		args := make([]string, len(e.Args))
		for i, a := range e.Args {
			args[i] = exprString(a)
		}
		return e.Name.Name + "(" + strings.Join(args, ", ") + ")"
	case *ast.ArrayExpr:
		elems := make([]string, len(e.Elems))
		for i, el := range e.Elems {
			elems[i] = exprString(el)
		}
		return "[" + strings.Join(elems, ", ") + "]"
	case *ast.BadExpr:
		return "<bad>"
	default:
		return fmt.Sprintf("<%T>", e)
	}
}

func TestParse_BuildsTheFullTree(t *testing.T) {
	src := `// Blog schema.
// Second line.
datasource db {
	provider = "postgres" // main
}

// Publication state.
enum PostStatus {
	draft // default state
	published

	@@map("post_status")
}

// floating comment

// Application account.
model User {
	id    Int      @id @default(autoincrement())
	email String?  @unique() @db.VarChar(255) // unique per tenant
	tags  String[] @go.type("models.Tags")
	score Float    @default(-1.5)
	admin Boolean  @default(true)

	// Composite speed index.
	@@index([email, score],
		name: "idx_user_email_score", where: "email IS NOT NULL")
}
`
	f, diags := parser.Parse("schema.tork", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("Parse reported diagnostics on well formed input:\n%s", strings.Join(diagStrings(diags), "\n"))
	}
	want := []string{
		"datasource db doc=\"Blog schema.\\nSecond line.\"",
		`  provider = "postgres" trail="main"`,
		`enum PostStatus doc="Publication state."`,
		`  draft trail="default state"`,
		"  published",
		`  @@map("post_status")`,
		`model User doc="Application account."`,
		"  field id Int attrs=[@id, @default(autoincrement())]",
		`  field email String? attrs=[@unique(), @db.VarChar(255)] trail="unique per tenant"`,
		`  field tags String[] attrs=[@go.type("models.Tags")]`,
		"  field score Float attrs=[@default(-1.5)]",
		"  field admin Boolean attrs=[@default(true)]",
		`  @@index([email, score], name: "idx_user_email_score", where: "email IS NOT NULL") doc="Composite speed index."`,
		`  floating "floating comment"`,
	}
	assertStrings(t, "dump", dumpFile(f), want)
}

func TestParse_EmptyFileYieldsEmptyTree(t *testing.T) {
	f, diags := parser.Parse("schema.tork", []byte("\n\n"))
	if len(diags) != 0 {
		t.Fatalf("Parse reported diagnostics: %v", diagStrings(diags))
	}
	if len(f.Decls) != 0 || len(f.Floating) != 0 {
		t.Errorf("Decls = %d, Floating = %d, want both 0", len(f.Decls), len(f.Floating))
	}
}

func TestParse_SingleLineBlocksAndTrailingCommas(t *testing.T) {
	src := `model B { id Int @id }
enum E { a }
model C {
	@@index([a, b,], name: "x",)
}
`
	f, diags := parser.Parse("schema.tork", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("Parse reported diagnostics:\n%s", strings.Join(diagStrings(diags), "\n"))
	}
	want := []string{
		"model B",
		"  field id Int attrs=[@id]",
		"enum E",
		"  a",
		"model C",
		`  @@index([a, b], name: "x")`,
	}
	assertStrings(t, "dump", dumpFile(f), want)
}

func TestParse_BlankLineDetachesCommentsInsideBodies(t *testing.T) {
	src := `model A {
	// floating inside

	// documents id
	id Int
}
`
	f, diags := parser.Parse("schema.tork", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("Parse reported diagnostics: %v", diagStrings(diags))
	}
	want := []string{
		"model A",
		`  field id Int doc="documents id"`,
		`  floating "floating inside"`,
	}
	assertStrings(t, "dump", dumpFile(f), want)
}

func TestParse_MultiArgumentCallsAndDanglingComments(t *testing.T) {
	src := `model A {
	id Int @default(fn("a", "b"))
	// dangling
}
`
	f, diags := parser.Parse("schema.tork", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("Parse reported diagnostics: %v", diagStrings(diags))
	}
	want := []string{
		"model A",
		`  field id Int attrs=[@default(fn("a", "b"))]`,
		`  floating "dangling"`,
	}
	assertStrings(t, "dump", dumpFile(f), want)
}

func TestCommentGroupText_NilGroupIsEmpty(t *testing.T) {
	var g *ast.CommentGroup
	if got := g.Text(); got != "" {
		t.Errorf("Text() on nil group = %q, want empty", got)
	}
}

func TestParse_RecordsSpans(t *testing.T) {
	src := "model User {\n\tid Int @id\n}\n"
	f, diags := parser.Parse("schema.tork", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("Parse reported diagnostics: %v", diagStrings(diags))
	}
	m := f.Decls[0].(*ast.ModelDecl)
	if want := (token.Span{Start: token.Pos{Offset: 0, Line: 1, Col: 1}, End: token.Pos{Offset: 26, Line: 3, Col: 2}}); m.Span != want {
		t.Errorf("model span = %+v, want %+v", m.Span, want)
	}
	fld := m.Fields[0]
	if fld.Span.Start != (token.Pos{Offset: 14, Line: 2, Col: 2}) {
		t.Errorf("field start = %+v, want 2:2", fld.Span.Start)
	}
	if fld.Span.End != (token.Pos{Offset: 24, Line: 2, Col: 12}) {
		t.Errorf("field end = %+v, want 2:12", fld.Span.End)
	}
	if fld.Type.Span.Start != (token.Pos{Offset: 17, Line: 2, Col: 5}) {
		t.Errorf("type start = %+v, want 2:5", fld.Type.Span.Start)
	}
	attr := fld.Attrs[0]
	if attr.Span.Start != (token.Pos{Offset: 21, Line: 2, Col: 9}) {
		t.Errorf("attr start = %+v, want 2:9", attr.Span.Start)
	}
}
