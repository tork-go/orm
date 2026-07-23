package analyze

import (
	"strconv"

	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/token"
)

// The helpers in this file turn attribute argument lists into typed
// values, reporting shape mistakes uniformly. Labels arrive with their
// sigil spelled out ("@map", "@@index", "@db.VarChar") so one helper
// serves field and block attributes without mangling either. They all
// treat a BadExpr as "already reported by the parser": the value is
// rejected but no second diagnostic is added, keeping one mistake at
// one diagnostic.

// spanOf returns an expression's span for anchoring diagnostics.
func spanOf(e ast.Expr) token.Span {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Span
	case *ast.StringLit:
		return e.Span
	case *ast.IntLit:
		return e.Span
	case *ast.FloatLit:
		return e.Span
	case *ast.BoolLit:
		return e.Span
	case *ast.FuncCall:
		return e.Span
	case *ast.ArrayExpr:
		return e.Span
	default:
		return e.(*ast.BadExpr).Span
	}
}

func isBad(e ast.Expr) bool {
	_, bad := e.(*ast.BadExpr)
	return bad
}

// noArgs enforces an attribute that takes none, such as @id. Empty
// parentheses are tolerated; the formatter strips them.
func (a *analyzer) noArgs(file, label string, span token.Span, args []*ast.AttrArg) bool {
	if len(args) > 0 {
		a.errorf(file, span, "%s takes no arguments", label)
		return false
	}
	return true
}

// positional rejects named arguments for attributes whose grammar is
// purely positional, so later helpers can ignore names entirely.
func (a *analyzer) positional(file, label string, args []*ast.AttrArg) bool {
	for _, arg := range args {
		if arg.Name != nil {
			a.errorf(file, arg.Span, "%s does not take named arguments", label)
			return false
		}
	}
	return true
}

// oneString extracts an attribute's single string argument, as in
// @map("author_id"). The example in the message shows the exact fix
// because the mistake is almost always a missing or misquoted value.
func (a *analyzer) oneString(file, label, example string, span token.Span, args []*ast.AttrArg) (string, token.Span, bool) {
	if !a.positional(file, label, args) {
		return "", span, false
	}
	if len(args) != 1 {
		a.errorf(file, span, "%s expects a string, e.g. %s(%s)", label, label, example)
		return "", span, false
	}
	lit, ok := args[0].Value.(*ast.StringLit)
	if !ok {
		if !isBad(args[0].Value) {
			a.errorf(file, spanOf(args[0].Value), "%s expects a string, e.g. %s(%s)", label, label, example)
		}
		return "", span, false
	}
	return lit.Value, lit.Span, true
}

// mapName handles @map and @@map, which share everything but their
// sigil: one string argument that must be usable as a SQL identifier.
func (a *analyzer) mapName(file, label, example string, span token.Span, args []*ast.AttrArg) (string, bool) {
	value, valueSpan, ok := a.oneString(file, label, `"`+example+`"`, span, args)
	if !ok {
		return "", false
	}
	if !isSQLIdent(value) {
		a.errorf(file, valueSpan, "%s value %q is not a valid identifier", label, value)
		return "", false
	}
	return value, true
}

// intArg reads one integer literal, reporting overflow here rather
// than in the parser so the message can carry attribute context.
func (a *analyzer) intArg(file, context string, e ast.Expr) (int, bool) {
	lit, ok := e.(*ast.IntLit)
	if !ok {
		if !isBad(e) {
			a.errorf(file, spanOf(e), "%s", context)
		}
		return 0, false
	}
	n, err := strconv.Atoi(lit.Text)
	if err != nil {
		a.errorf(file, lit.Span, "integer %s is out of range", lit.Text)
		return 0, false
	}
	return n, true
}

// identList reads a [a, b, c] argument into the bare names, as used by
// @@id, @@unique, @@index, fields:, and references:.
func (a *analyzer) identList(file, context string, e ast.Expr) ([]ast.Ident, bool) {
	arr, ok := e.(*ast.ArrayExpr)
	if !ok {
		if !isBad(e) {
			a.errorf(file, spanOf(e), "%s", context)
		}
		return nil, false
	}
	idents := make([]ast.Ident, 0, len(arr.Elems))
	for _, el := range arr.Elems {
		id, ok := el.(*ast.Ident)
		if !ok {
			if !isBad(el) {
				a.errorf(file, spanOf(el), "%s", context)
			}
			return nil, false
		}
		idents = append(idents, *id)
	}
	return idents, true
}

// stringList reads a ["a", "b"] argument, as used by @@index's on:.
func (a *analyzer) stringList(file, context string, e ast.Expr) ([]string, bool) {
	arr, ok := e.(*ast.ArrayExpr)
	if !ok {
		if !isBad(e) {
			a.errorf(file, spanOf(e), "%s", context)
		}
		return nil, false
	}
	out := make([]string, 0, len(arr.Elems))
	for _, el := range arr.Elems {
		lit, ok := el.(*ast.StringLit)
		if !ok {
			if !isBad(el) {
				a.errorf(file, spanOf(el), "%s", context)
			}
			return nil, false
		}
		if lit.Value == "" {
			a.errorf(file, lit.Span, "%s", context)
			return nil, false
		}
		out = append(out, lit.Value)
	}
	return out, true
}
