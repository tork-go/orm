package parser

import (
	"fmt"

	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/diag"
	"github.com/tork-go/orm/gen/token"
)

// Parse turns one .tork source file into a syntax tree. Like Scan it
// never fails: any malformation is reported as a diagnostic while the
// parser resynchronizes and keeps going, so the returned File always
// holds every declaration that could be shaped from the input. The
// generator refuses to generate when the diagnostics contain an error;
// the language server works off the partial tree regardless.
func Parse(file string, src []byte) (*ast.File, []diag.Diagnostic) {
	toks, diags := Scan(file, src)
	p := &parser{file: file, toks: toks, diags: diags}
	return p.parseFile(), p.diags
}

// parser is a recursive descent walk over a fully scanned token slice.
// Lexing everything up front costs nothing at schema sizes and buys
// unbounded lookahead, which keeps every recovery decision a plain
// index comparison.
type parser struct {
	file  string
	toks  []token.Token
	i     int
	diags []diag.Diagnostic
}

func (p *parser) tok() token.Token { return p.toks[p.i] }

func (p *parser) at(k token.Kind) bool { return p.toks[p.i].Kind == k }

// next consumes the current token. It refuses to move past EOF, so a
// runaway loop degenerates into spinning on EOF instead of panicking,
// and every loop in this file breaks on EOF.
func (p *parser) next() token.Token {
	t := p.toks[p.i]
	if t.Kind != token.KindEOF {
		p.i++
	}
	return t
}

// prevEnd is the end position of the last consumed token: the natural
// end of whatever node just finished parsing. Every caller sits behind
// at least one next() on its path (parseFile consumes nothing itself),
// so the index is never zero here.
func (p *parser) prevEnd() token.Pos {
	return p.toks[p.i-1].Span.End
}

func (p *parser) errorf(span token.Span, format string, args ...any) {
	p.diags = append(p.diags, diag.Errorf(p.file, span, format, args...))
}

// describe names a token the way an error message should: literals by
// their spelling, structure by its symbol, and the two invisible tokens
// by what the user actually sees.
func describe(t token.Token) string {
	switch t.Kind {
	case token.KindNewline:
		return "end of line"
	case token.KindEOF:
		return "end of file"
	case token.KindIdent, token.KindInt, token.KindFloat, token.KindIllegal:
		return fmt.Sprintf("%q", t.Lit)
	case token.KindString:
		return fmt.Sprintf("string %q", t.Lit)
	default:
		return fmt.Sprintf("%q", t.Kind.String())
	}
}

func isTopKeyword(word string) bool {
	switch word {
	case "model", "enum", "datasource", "generator":
		return true
	}
	return false
}

// collectComments consumes the newlines and comments before the next
// construct and buckets the comments into groups, splitting on blank
// lines. Whether the final group documents the next construct is
// splitDoc's decision; this function only gathers.
func (p *parser) collectComments() []ast.CommentGroup {
	var groups []ast.CommentGroup
	var cur []ast.Comment
	lastLine := -2
	flush := func() {
		if len(cur) > 0 {
			groups = append(groups, ast.CommentGroup{Comments: cur})
			cur = nil
		}
	}
	for {
		switch t := p.tok(); t.Kind {
		case token.KindNewline:
			p.next()
		case token.KindComment:
			if t.Span.Start.Line > lastLine+1 {
				flush()
			}
			cur = append(cur, ast.Comment{Text: t.Lit, Span: t.Span})
			lastLine = t.Span.Start.Line
			p.next()
		default:
			flush()
			return groups
		}
	}
}

// splitDoc attaches the last collected group to the construct that
// starts on the very next line, following the same adjacency rule
// go/ast uses; everything else floats. A blank line between a comment
// and a declaration is the author saying "these are not attached", and
// the formatter preserves that.
func (p *parser) splitDoc(groups []ast.CommentGroup, floating *[]ast.Comment) *ast.CommentGroup {
	if len(groups) == 0 {
		return nil
	}
	var doc *ast.CommentGroup
	n := len(groups)
	last := groups[n-1]
	lastLine := last.Comments[len(last.Comments)-1].Span.Start.Line
	if p.tok().Span.Start.Line == lastLine+1 {
		doc = &last
		n--
	}
	for _, g := range groups[:n] {
		*floating = append(*floating, g.Comments...)
	}
	return doc
}

func floatAll(groups []ast.CommentGroup, dst *[]ast.Comment) {
	for _, g := range groups {
		*dst = append(*dst, g.Comments...)
	}
}

func (p *parser) parseFile() *ast.File {
	f := &ast.File{Name: p.file}
	start := p.tok().Span.Start
	for {
		groups := p.collectComments()
		if p.at(token.KindEOF) {
			floatAll(groups, &f.Floating)
			break
		}
		doc := p.splitDoc(groups, &f.Floating)
		f.Decls = append(f.Decls, p.parseDecl(doc))
	}
	f.Span = token.Span{Start: start, End: p.tok().Span.End}
	return f
}

func (p *parser) parseDecl(doc *ast.CommentGroup) ast.Decl {
	t := p.tok()
	if t.Kind == token.KindIdent {
		switch t.Lit {
		case "model":
			return p.parseModel(doc)
		case "enum":
			return p.parseEnum(doc)
		case "datasource":
			return p.parseDatasource(doc)
		case "generator":
			p.errorf(t.Span, `unsupported block "generator" (tork is configured in Go; wire gen/cli into your cmd/generate instead)`)
			return p.badDeclFrom(t.Span.Start)
		}
	}
	p.errorf(t.Span, "unexpected %s at top level (expected model, enum, or datasource)", describe(t))
	return p.badDeclFrom(t.Span.Start)
}

// badDeclFrom skips forward to the next plausible declaration and wraps
// everything skipped in a BadDecl. The resynchronization point is a top
// level keyword at the start of a line outside any braces; anything
// weaker resynchronizes inside the block that just failed and shreds
// the rest of the file into nonsense diagnostics.
func (p *parser) badDeclFrom(start token.Pos) *ast.BadDecl {
	depth := 0
	afterNewline := false
	for {
		t := p.tok()
		if t.Kind == token.KindEOF {
			break
		}
		if depth == 0 && afterNewline && t.Kind == token.KindIdent && isTopKeyword(t.Lit) {
			break
		}
		switch t.Kind {
		case token.KindLBrace:
			depth++
		case token.KindRBrace:
			if depth > 0 {
				depth--
			}
		}
		afterNewline = t.Kind == token.KindNewline
		p.next()
	}
	return &ast.BadDecl{Span: token.Span{Start: start, End: p.prevEnd()}}
}

// expectIdent consumes and returns an identifier, or reports what stood
// in its place. It does not consume the offender; the caller owns
// recovery and knows the better synchronization point.
func (p *parser) expectIdent(what string) (ast.Ident, bool) {
	if p.at(token.KindIdent) {
		t := p.next()
		return ast.Ident{Name: t.Lit, Span: t.Span}, true
	}
	p.errorf(p.tok().Span, "expected %s, found %s", what, describe(p.tok()))
	return ast.Ident{}, false
}

func (p *parser) expect(kind token.Kind, what string) bool {
	if p.at(kind) {
		p.next()
		return true
	}
	p.errorf(p.tok().Span, "expected %s, found %s", what, describe(p.tok()))
	return false
}

func (p *parser) parseModel(doc *ast.CommentGroup) ast.Decl {
	kw := p.next()
	name, ok := p.expectIdent(`a model name after "model"`)
	if !ok {
		return p.badDeclFrom(kw.Span.Start)
	}
	if !p.expect(token.KindLBrace, fmt.Sprintf("{ to open model %q", name.Name)) {
		return p.badDeclFrom(kw.Span.Start)
	}
	m := &ast.ModelDecl{Doc: doc, Name: name}
	for {
		groups := p.collectComments()
		t := p.tok()
		switch {
		case t.Kind == token.KindRBrace:
			floatAll(groups, &m.Floating)
			p.next()
			m.Span = token.Span{Start: kw.Span.Start, End: p.prevEnd()}
			return m
		case t.Kind == token.KindEOF:
			floatAll(groups, &m.Floating)
			p.errorf(t.Span, "model %q is missing its closing }", m.Name.Name)
			m.Span = token.Span{Start: kw.Span.Start, End: p.prevEnd()}
			return m
		case t.Kind == token.KindIdent && isTopKeyword(t.Lit) && t.Span.Start.Col == 1:
			// A top level keyword at column one inside a body almost
			// always means the closing brace above was never typed.
			// Handing the keyword back gives the next declaration a
			// clean parse instead of shredding it into fake fields.
			floatAll(groups, &m.Floating)
			p.errorf(t.Span, "model %q is missing its closing }", m.Name.Name)
			m.Span = token.Span{Start: kw.Span.Start, End: p.prevEnd()}
			return m
		case t.Kind == token.KindAtAt:
			m.Attrs = append(m.Attrs, p.parseBlockAttribute(p.splitDoc(groups, &m.Floating)))
		case t.Kind == token.KindIdent:
			m.Fields = append(m.Fields, p.parseField(p.splitDoc(groups, &m.Floating)))
		default:
			floatAll(groups, &m.Floating)
			p.errorf(t.Span, "expected a field or a @@ attribute in model %q, found %s", m.Name.Name, describe(t))
			p.skipLine()
		}
	}
}

func (p *parser) parseEnum(doc *ast.CommentGroup) ast.Decl {
	kw := p.next()
	name, ok := p.expectIdent(`an enum name after "enum"`)
	if !ok {
		return p.badDeclFrom(kw.Span.Start)
	}
	if !p.expect(token.KindLBrace, fmt.Sprintf("{ to open enum %q", name.Name)) {
		return p.badDeclFrom(kw.Span.Start)
	}
	e := &ast.EnumDecl{Doc: doc, Name: name}
	for {
		groups := p.collectComments()
		t := p.tok()
		switch {
		case t.Kind == token.KindRBrace:
			floatAll(groups, &e.Floating)
			p.next()
			e.Span = token.Span{Start: kw.Span.Start, End: p.prevEnd()}
			return e
		case t.Kind == token.KindEOF:
			floatAll(groups, &e.Floating)
			p.errorf(t.Span, "enum %q is missing its closing }", e.Name.Name)
			e.Span = token.Span{Start: kw.Span.Start, End: p.prevEnd()}
			return e
		case t.Kind == token.KindIdent && isTopKeyword(t.Lit) && t.Span.Start.Col == 1:
			floatAll(groups, &e.Floating)
			p.errorf(t.Span, "enum %q is missing its closing }", e.Name.Name)
			e.Span = token.Span{Start: kw.Span.Start, End: p.prevEnd()}
			return e
		case t.Kind == token.KindAtAt:
			e.Attrs = append(e.Attrs, p.parseBlockAttribute(p.splitDoc(groups, &e.Floating)))
		case t.Kind == token.KindIdent:
			v := &ast.EnumValue{Doc: p.splitDoc(groups, &e.Floating)}
			nameTok := p.next()
			v.Name = ast.Ident{Name: nameTok.Lit, Span: nameTok.Span}
			v.Span = nameTok.Span
			p.finishLine(&v.Trailing, fmt.Sprintf("after enum value %q", v.Name.Name))
			e.Values = append(e.Values, v)
		default:
			floatAll(groups, &e.Floating)
			p.errorf(t.Span, "expected an enum value or a @@ attribute in enum %q, found %s", e.Name.Name, describe(t))
			p.skipLine()
		}
	}
}

func (p *parser) parseDatasource(doc *ast.CommentGroup) ast.Decl {
	kw := p.next()
	name, ok := p.expectIdent(`a datasource name after "datasource"`)
	if !ok {
		return p.badDeclFrom(kw.Span.Start)
	}
	if !p.expect(token.KindLBrace, fmt.Sprintf("{ to open datasource %q", name.Name)) {
		return p.badDeclFrom(kw.Span.Start)
	}
	d := &ast.DatasourceDecl{Doc: doc, Name: name}
	for {
		groups := p.collectComments()
		floatAll(groups, &d.Floating)
		t := p.tok()
		switch {
		case t.Kind == token.KindRBrace:
			p.next()
			d.Span = token.Span{Start: kw.Span.Start, End: p.prevEnd()}
			return d
		case t.Kind == token.KindEOF:
			p.errorf(t.Span, "datasource %q is missing its closing }", d.Name.Name)
			d.Span = token.Span{Start: kw.Span.Start, End: p.prevEnd()}
			return d
		case t.Kind == token.KindIdent && isTopKeyword(t.Lit) && t.Span.Start.Col == 1:
			p.errorf(t.Span, "datasource %q is missing its closing }", d.Name.Name)
			d.Span = token.Span{Start: kw.Span.Start, End: p.prevEnd()}
			return d
		case t.Kind == token.KindIdent:
			entry := &ast.DatasourceEntry{}
			keyTok := p.next()
			entry.Key = ast.Ident{Name: keyTok.Lit, Span: keyTok.Span}
			if !p.expect(token.KindAssign, fmt.Sprintf("= after %q in datasource %q", entry.Key.Name, d.Name.Name)) {
				p.skipLine()
				continue
			}
			entry.Value = p.parseExpr(fmt.Sprintf("datasource %q", d.Name.Name))
			entry.Span = token.Span{Start: keyTok.Span.Start, End: p.prevEnd()}
			p.finishLine(&entry.Trailing, fmt.Sprintf("after the %q entry", entry.Key.Name))
			d.Entries = append(d.Entries, entry)
		default:
			p.errorf(t.Span, "expected a key = value entry in datasource %q, found %s", d.Name.Name, describe(t))
			p.skipLine()
		}
	}
}

func (p *parser) parseField(doc *ast.CommentGroup) *ast.FieldDecl {
	fld := &ast.FieldDecl{Doc: doc}
	nameTok := p.next()
	fld.Name = ast.Ident{Name: nameTok.Lit, Span: nameTok.Span}
	if p.at(token.KindIdent) {
		fld.Type = p.parseTypeRef()
	} else {
		p.errorf(nameTok.Span, "field %q is missing a type", nameTok.Lit)
	}
	for p.at(token.KindAt) {
		fld.Attrs = append(fld.Attrs, p.parseAttribute())
	}
	fld.Span = token.Span{Start: nameTok.Span.Start, End: p.prevEnd()}
	p.finishLine(&fld.Trailing, fmt.Sprintf("after field %q", fld.Name.Name))
	return fld
}

func (p *parser) parseTypeRef() ast.TypeRef {
	nameTok := p.next()
	tr := ast.TypeRef{Name: ast.Ident{Name: nameTok.Lit, Span: nameTok.Span}}
	end := nameTok.Span.End
	if p.at(token.KindLBracket) && p.toks[p.i+1].Kind == token.KindRBracket {
		p.next()
		end = p.next().Span.End
		tr.List = true
	}
	if p.at(token.KindQuestion) {
		end = p.next().Span.End
		tr.Optional = true
	}
	tr.Span = token.Span{Start: nameTok.Span.Start, End: end}
	return tr
}

func (p *parser) parseAttribute() *ast.Attribute {
	atTok := p.next()
	a := &ast.Attribute{}
	if !p.at(token.KindIdent) {
		p.errorf(atTok.Span, "expected an attribute name after @")
		a.Span = atTok.Span
		return a
	}
	nameTok := p.next()
	a.Parts = append(a.Parts, ast.Ident{Name: nameTok.Lit, Span: nameTok.Span})
	for p.at(token.KindDot) {
		p.next()
		if !p.at(token.KindIdent) {
			p.errorf(p.tok().Span, "expected an identifier after . in attribute @%s", a.Name())
			break
		}
		t := p.next()
		a.Parts = append(a.Parts, ast.Ident{Name: t.Lit, Span: t.Span})
	}
	if p.at(token.KindLParen) {
		a.HasParens = true
		a.Args = p.parseArgs("@" + a.Name())
	}
	a.Span = token.Span{Start: atTok.Span.Start, End: p.prevEnd()}
	return a
}

func (p *parser) parseBlockAttribute(doc *ast.CommentGroup) *ast.BlockAttribute {
	atatTok := p.next()
	b := &ast.BlockAttribute{Doc: doc}
	if !p.at(token.KindIdent) {
		p.errorf(atatTok.Span, "expected an attribute name after @@")
		b.Span = atatTok.Span
		p.skipLine()
		return b
	}
	nameTok := p.next()
	b.Name = ast.Ident{Name: nameTok.Lit, Span: nameTok.Span}
	if p.at(token.KindLParen) {
		b.HasParens = true
		b.Args = p.parseArgs("@@" + b.Name.Name)
	}
	b.Span = token.Span{Start: atatTok.Span.Start, End: p.prevEnd()}
	p.finishLine(&b.Trailing, fmt.Sprintf("after @@%s", b.Name.Name))
	return b
}

// finishLine closes out a one line construct: an optional trailing
// comment, then the line break. Anything else on the line is reported
// once and skipped, so a single stray token costs a single diagnostic.
func (p *parser) finishLine(trailing **ast.Comment, context string) {
	if p.at(token.KindComment) {
		t := p.next()
		*trailing = &ast.Comment{Text: t.Lit, Span: t.Span}
	}
	switch p.tok().Kind {
	case token.KindNewline:
		p.next()
	case token.KindRBrace, token.KindEOF:
	default:
		t := p.tok()
		p.errorf(t.Span, "unexpected %s %s", describe(t), context)
		p.skipLine()
	}
}

// skipLine consumes through the end of the current line, stopping short
// of a closing brace or end of file so the enclosing block still sees
// its terminator.
func (p *parser) skipLine() {
	for {
		switch p.tok().Kind {
		case token.KindEOF, token.KindRBrace:
			return
		case token.KindNewline:
			p.next()
			return
		}
		p.next()
	}
}

// skipInsideParens consumes newlines and comments inside parentheses,
// where line breaks are insignificant: this is the one place the
// grammar lets a construct span lines.
func (p *parser) skipInsideParens() {
	for p.at(token.KindNewline) || p.at(token.KindComment) {
		p.next()
	}
}

// parseArgs consumes a parenthesized argument list, already positioned
// on the opening parenthesis. On a missing closing parenthesis it bails
// at the first token that cannot be part of an argument (a brace, an
// attribute sigil, end of file) without consuming it, so an unclosed
// list swallows at most its own line rather than the rest of the file.
func (p *parser) parseArgs(owner string) []*ast.AttrArg {
	p.next()
	args := []*ast.AttrArg{}
	for {
		p.skipInsideParens()
		switch p.tok().Kind {
		case token.KindRParen:
			p.next()
			return args
		case token.KindEOF, token.KindRBrace, token.KindAt, token.KindAtAt:
			p.errorf(p.tok().Span, "missing ) to close the arguments of %s", owner)
			return args
		}
		arg := &ast.AttrArg{}
		start := p.tok().Span.Start
		if p.at(token.KindIdent) && p.toks[p.i+1].Kind == token.KindColon {
			nameTok := p.next()
			p.next()
			arg.Name = &ast.Ident{Name: nameTok.Lit, Span: nameTok.Span}
			p.skipInsideParens()
		}
		arg.Value = p.parseExpr(owner)
		arg.Span = token.Span{Start: start, End: p.prevEnd()}
		args = append(args, arg)
		p.skipInsideParens()
		switch p.tok().Kind {
		case token.KindComma:
			p.next()
		case token.KindRParen, token.KindEOF, token.KindRBrace, token.KindAt, token.KindAtAt:
			// The loop top decides between closing and bailing.
		default:
			t := p.tok()
			p.errorf(t.Span, "expected , or ) in the arguments of %s, found %s", owner, describe(t))
			p.next()
		}
	}
}

// isExprBoundary reports whether a token can never begin a value and
// belongs to the surrounding structure. parseExpr leaves these alone so
// the caller's recovery sees them.
func isExprBoundary(k token.Kind) bool {
	switch k {
	case token.KindComma, token.KindRParen, token.KindRBracket, token.KindRBrace,
		token.KindAt, token.KindAtAt, token.KindNewline, token.KindEOF:
		return true
	}
	return false
}

func (p *parser) parseExpr(owner string) ast.Expr {
	t := p.tok()
	switch t.Kind {
	case token.KindString:
		p.next()
		return &ast.StringLit{Value: t.Lit, Span: t.Span}
	case token.KindInt:
		p.next()
		return &ast.IntLit{Text: t.Lit, Span: t.Span}
	case token.KindFloat:
		p.next()
		return &ast.FloatLit{Text: t.Lit, Span: t.Span}
	case token.KindIdent:
		p.next()
		if t.Lit == "true" || t.Lit == "false" {
			return &ast.BoolLit{Value: t.Lit == "true", Span: t.Span}
		}
		id := ast.Ident{Name: t.Lit, Span: t.Span}
		if p.at(token.KindLParen) {
			return p.parseCall(id, owner)
		}
		return &id
	case token.KindLBracket:
		return p.parseArray(owner)
	default:
		p.errorf(t.Span, "expected a value in %s, found %s", owner, describe(t))
		if !isExprBoundary(t.Kind) {
			p.next()
		}
		return &ast.BadExpr{Span: t.Span}
	}
}

func (p *parser) parseCall(name ast.Ident, owner string) ast.Expr {
	p.next()
	call := &ast.FuncCall{Name: name}
	for {
		p.skipInsideParens()
		switch p.tok().Kind {
		case token.KindRParen:
			p.next()
			call.Span = token.Span{Start: name.Span.Start, End: p.prevEnd()}
			return call
		case token.KindEOF, token.KindRBrace, token.KindAt, token.KindAtAt:
			p.errorf(p.tok().Span, "missing ) to close the call %s(...) in %s", name.Name, owner)
			call.Span = token.Span{Start: name.Span.Start, End: p.prevEnd()}
			return call
		}
		call.Args = append(call.Args, p.parseExpr(owner))
		p.skipInsideParens()
		switch p.tok().Kind {
		case token.KindComma:
			p.next()
		case token.KindRParen, token.KindEOF, token.KindRBrace, token.KindAt, token.KindAtAt:
		default:
			t := p.tok()
			p.errorf(t.Span, "expected , or ) in the call %s(...), found %s", name.Name, describe(t))
			p.next()
		}
	}
}

func (p *parser) parseArray(owner string) ast.Expr {
	openTok := p.next()
	arr := &ast.ArrayExpr{}
	for {
		p.skipInsideParens()
		switch p.tok().Kind {
		case token.KindRBracket:
			p.next()
			arr.Span = token.Span{Start: openTok.Span.Start, End: p.prevEnd()}
			return arr
		case token.KindEOF, token.KindRBrace, token.KindRParen, token.KindAt, token.KindAtAt:
			p.errorf(p.tok().Span, "missing ] to close the list in %s", owner)
			arr.Span = token.Span{Start: openTok.Span.Start, End: p.prevEnd()}
			return arr
		}
		arr.Elems = append(arr.Elems, p.parseExpr(owner))
		p.skipInsideParens()
		switch p.tok().Kind {
		case token.KindComma:
			p.next()
		case token.KindRBracket, token.KindEOF, token.KindRBrace, token.KindRParen, token.KindAt, token.KindAtAt:
		default:
			t := p.tok()
			p.errorf(t.Span, "expected , or ] in the list in %s, found %s", owner, describe(t))
			p.next()
		}
	}
}
