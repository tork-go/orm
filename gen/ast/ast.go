package ast

import (
	"strings"

	"github.com/tork-go/orm/gen/token"
)

// File is one parsed .tork source file. Name is the path exactly as it
// was handed to the parser; the parser never opens files itself, which
// is what lets the language server parse unsaved editor buffers.
type File struct {
	Name     string
	Decls    []Decl
	Floating []Comment
	Span     token.Span
}

// Comment is one line comment. Text is everything after the two slashes,
// verbatim including leading whitespace, so the formatter decides on a
// canonical spacing without the parser having destroyed the original.
type Comment struct {
	Text string
	Span token.Span
}

// CommentGroup is a run of comments on consecutive lines with no blank
// line between them, attached as documentation to the declaration or
// member that directly follows.
type CommentGroup struct {
	Comments []Comment
}

// Text joins the group into display text, one line per comment with the
// surrounding whitespace trimmed. This is what the language server shows
// on hover, mirroring how godoc treats a doc comment.
func (g *CommentGroup) Text() string {
	if g == nil {
		return ""
	}
	lines := make([]string, 0, len(g.Comments))
	for _, c := range g.Comments {
		lines = append(lines, strings.TrimSpace(c.Text))
	}
	return strings.Join(lines, "\n")
}

// Ident is a name with its position: a model name, field name, attribute
// word, or a bare word in expression position such as an enum member.
type Ident struct {
	Name string
	Span token.Span
}

// Decl is a top level declaration. The variants are ModelDecl, EnumDecl,
// DatasourceDecl, and BadDecl.
type Decl interface{ declNode() }

func (*ModelDecl) declNode()      {}
func (*EnumDecl) declNode()       {}
func (*DatasourceDecl) declNode() {}
func (*BadDecl) declNode()        {}

// ModelDecl is one "model Name { ... }" block. Fields and block
// attributes keep their own declaration order; the two slices are
// separate because no consumer ever wants them mixed, and the formatter
// canonicalizes fields before attributes anyway.
type ModelDecl struct {
	Doc      *CommentGroup
	Name     Ident
	Fields   []*FieldDecl
	Attrs    []*BlockAttribute
	Floating []Comment
	Span     token.Span
}

// EnumDecl is one "enum Name { ... }" block.
type EnumDecl struct {
	Doc      *CommentGroup
	Name     Ident
	Values   []*EnumValue
	Attrs    []*BlockAttribute
	Floating []Comment
	Span     token.Span
}

// EnumValue is one member line inside an enum block.
type EnumValue struct {
	Doc      *CommentGroup
	Name     Ident
	Trailing *Comment
	Span     token.Span
}

// DatasourceDecl is one "datasource name { key = value ... }" block.
type DatasourceDecl struct {
	Doc      *CommentGroup
	Name     Ident
	Entries  []*DatasourceEntry
	Floating []Comment
	Span     token.Span
}

// DatasourceEntry is one "key = value" line. Value is an Expr rather
// than a string because the parser stays tolerant; the analyzer is the
// one to insist on a string literal.
type DatasourceEntry struct {
	Key      Ident
	Value    Expr
	Trailing *Comment
	Span     token.Span
}

// BadDecl covers source the parser could not shape into a declaration.
// It exists so a broken region still occupies its span in the tree; the
// language server can then tell "broken code here" apart from "no code
// here" when deciding what to complete.
type BadDecl struct {
	Span token.Span
}

// FieldDecl is one field line inside a model. A missing type parses as
// a TypeRef with an empty name, alongside a diagnostic, because a half
// typed field is exactly the moment completion is asked to help.
type FieldDecl struct {
	Doc      *CommentGroup
	Name     Ident
	Type     TypeRef
	Attrs    []*Attribute
	Trailing *Comment
	Span     token.Span
}

// TypeRef is a field's type: a name, optionally a list, optionally
// optional, in exactly that order ("String[]?").
type TypeRef struct {
	Name     Ident
	List     bool
	Optional bool
	Span     token.Span
}

// String renders the type as it is spelled in source, for diagnostics
// and hovers.
func (t TypeRef) String() string {
	s := t.Name.Name
	if t.List {
		s += "[]"
	}
	if t.Optional {
		s += "?"
	}
	return s
}

// Attribute is one field attribute: @id, @db.VarChar(30). Parts holds
// the dotted path; HasParens distinguishes "@id" from "@id()", which the
// formatter normalizes but the parser must not conflate.
type Attribute struct {
	Parts     []Ident
	Args      []*AttrArg
	HasParens bool
	Span      token.Span
}

// Name joins the dotted attribute path, without the @ sigil.
func (a *Attribute) Name() string {
	parts := make([]string, len(a.Parts))
	for i, p := range a.Parts {
		parts[i] = p.Name
	}
	return strings.Join(parts, ".")
}

// BlockAttribute is one @@ attribute line inside a model or enum body.
type BlockAttribute struct {
	Doc       *CommentGroup
	Name      Ident
	Args      []*AttrArg
	HasParens bool
	Trailing  *Comment
	Span      token.Span
}

// AttrArg is one argument in an attribute's parentheses, positional when
// Name is nil and named ("fields: [...]") otherwise.
type AttrArg struct {
	Name  *Ident
	Value Expr
	Span  token.Span
}

// Expr is a value in attribute argument position. The variants are
// Ident, StringLit, IntLit, FloatLit, BoolLit, FuncCall, ArrayExpr, and
// BadExpr.
type Expr interface{ exprNode() }

func (*Ident) exprNode()     {}
func (*StringLit) exprNode() {}
func (*IntLit) exprNode()    {}
func (*FloatLit) exprNode()  {}
func (*BoolLit) exprNode()   {}
func (*FuncCall) exprNode()  {}
func (*ArrayExpr) exprNode() {}
func (*BadExpr) exprNode()   {}

// StringLit is a double quoted literal; Value holds the decoded text.
type StringLit struct {
	Value string
	Span  token.Span
}

// IntLit keeps its source spelling. The analyzer converts it where a
// number is needed, so range errors are reported once, with schema
// context the parser does not have.
type IntLit struct {
	Text string
	Span token.Span
}

// FloatLit keeps its source spelling, for the same reason IntLit does.
type FloatLit struct {
	Text string
	Span token.Span
}

// BoolLit is a bare true or false in expression position.
type BoolLit struct {
	Value bool
	Span  token.Span
}

// FuncCall is a call form such as autoincrement(), now(), or
// go("path.Func"). The known function names are the analyzer's business;
// the parser accepts any.
type FuncCall struct {
	Name Ident
	Args []Expr
	Span token.Span
}

// ArrayExpr is a bracketed list, as in @@index([authorId, createdAt]).
type ArrayExpr struct {
	Elems []Expr
	Span  token.Span
}

// BadExpr stands where a value should have been but was not. It keeps
// argument positions aligned, so one bad argument does not shift the
// meaning of the ones after it.
type BadExpr struct {
	Span token.Span
}
