package analyze

import (
	"strconv"
	"strings"

	"github.com/tork-go/orm/gen/ast"
)

// defaultFuncNames is the suggestion catalog for @default call forms.
var defaultFuncNames = []string{"autoincrement", "now", "uuid", "dbgenerated", "go"}

// defaultAttr applies @default. The forms split three ways at
// generation time: server side expressions become ServerDefault,
// uuid() and go() become GeneratedByClient, and autoincrement() emits
// nothing because the ORM derives identity from the key shape; the
// analyzer's job is deciding which interpretation a given value gets
// and rejecting the rest with the fix in the message.
func (a *analyzer) defaultAttr(m *Model, f *Field, attr *ast.Attribute) {
	if f.Type.Kind == TypeModel {
		a.errorf(m.File, attr.Span, "@default cannot apply to a relation field")
		return
	}
	if !a.positional(m.File, "@default", attr.Args) {
		return
	}
	if len(attr.Args) == 0 {
		a.errorf(m.File, attr.Span, "@default requires a value, e.g. @default(now())")
		return
	}
	if len(attr.Args) > 1 {
		a.errorf(m.File, attr.Span, "@default takes one argument")
		return
	}
	value := attr.Args[0].Value
	span := spanOf(value)
	def := a.defaultValue(m, f, value)
	if def != nil {
		def.Span = span
		f.Default = def
	}
}

func (a *analyzer) defaultValue(m *Model, f *Field, value ast.Expr) *Default {
	switch value := value.(type) {
	case *ast.FuncCall:
		return a.defaultCall(m, f, value)
	case *ast.Ident:
		return a.defaultIdent(m, f, value)
	case *ast.IntLit, *ast.FloatLit, *ast.StringLit, *ast.BoolLit:
		return a.defaultLiteral(m, f, value)
	case *ast.ArrayExpr:
		a.errorf(m.File, value.Span, "@default does not accept list literals; use dbgenerated(...)")
		return nil
	default:
		return nil
	}
}

// defaultIdent handles a bare word: an enum member, or a function name
// missing its parentheses, which deserves the exact fix.
func (a *analyzer) defaultIdent(m *Model, f *Field, id *ast.Ident) *Default {
	for _, fn := range defaultFuncNames {
		if id.Name == fn {
			a.errorf(m.File, id.Span, "%s is a function; write @default(%s())", fn, fn)
			return nil
		}
	}
	// An enum field can never be a list (rejected at type resolution),
	// so a bare member name needs no list guard here.
	if f.Type.Kind == TypeEnum {
		for _, v := range f.Type.Enum.Values {
			if v == id.Name {
				return &Default{Kind: DefaultLiteral, SQL: sqlQuote(v)}
			}
		}
		a.errorf(m.File, id.Span, "%q is not a value of enum %s%s", id.Name, f.Type.Enum.Name, suggestion(id.Name, f.Type.Enum.Values))
		return nil
	}
	a.errorf(m.File, id.Span, "unknown default %q%s", id.Name, suggestion(id.Name, defaultFuncNames))
	return nil
}

func (a *analyzer) defaultCall(m *Model, f *Field, call *ast.FuncCall) *Default {
	file := m.File
	switch call.Name.Name {
	case "dbgenerated":
		// The one form allowed on every field type, including lists
		// and Json: the expression is the user's own SQL, passed
		// through verbatim.
		if len(call.Args) != 1 {
			a.errorf(file, call.Span, `dbgenerated() requires a SQL expression, e.g. dbgenerated("now()")`)
			return nil
		}
		lit, ok := call.Args[0].(*ast.StringLit)
		if !ok || lit.Value == "" {
			if ok || !isBad(call.Args[0]) {
				a.errorf(file, spanOf(call.Args[0]), `dbgenerated() requires a SQL expression, e.g. dbgenerated("now()")`)
			}
			return nil
		}
		return &Default{Kind: DefaultDBGenerated, SQL: lit.Value}
	case "go":
		if len(call.Args) != 1 {
			a.errorf(file, call.Span, `go() requires a function reference, e.g. go("mypkg.NewID")`)
			return nil
		}
		lit, ok := call.Args[0].(*ast.StringLit)
		if !ok {
			if !isBad(call.Args[0]) {
				a.errorf(file, spanOf(call.Args[0]), `go() requires a function reference, e.g. go("mypkg.NewID")`)
			}
			return nil
		}
		ref, ok := parseGoRef(lit.Value)
		if !ok {
			a.errorf(file, lit.Span, `invalid Go function reference %q (write "Name" for the generated package, or "import/path.Name")`, lit.Value)
			return nil
		}
		return &Default{Kind: DefaultGoFunc, GoFunc: ref}
	}

	if f.List {
		a.errorf(file, call.Span, "@default on a list field supports only dbgenerated(...)")
		return nil
	}
	switch call.Name.Name {
	case "autoincrement":
		if len(call.Args) > 0 {
			a.errorf(file, call.Span, "autoincrement() takes no arguments")
			return nil
		}
		if !isIntegerKind(f.Type.Kind) {
			a.errorf(file, call.Span, "@default(autoincrement()) requires an integer field")
			return nil
		}
		return &Default{Kind: DefaultAutoincrement}
	case "now":
		if len(call.Args) > 0 {
			a.errorf(file, call.Span, "now() takes no arguments")
			return nil
		}
		if f.Type.Kind != TypeDateTime {
			a.errorf(file, call.Span, "now() requires a DateTime field")
			return nil
		}
		return &Default{Kind: DefaultNow, SQL: "now()"}
	case "uuid":
		if len(call.Args) > 0 {
			a.errorf(file, call.Span, "uuid() takes no arguments")
			return nil
		}
		if f.Type.Kind != TypeUuid {
			a.errorf(file, call.Span, "uuid() requires a Uuid field")
			return nil
		}
		return &Default{Kind: DefaultUUID, GoFunc: GoTypeRef{ImportPath: "github.com/google/uuid", Name: "New"}}
	default:
		a.errorf(file, call.Name.Span, "unknown default %q%s", call.Name.Name, suggestion(call.Name.Name, defaultFuncNames))
		return nil
	}
}

// defaultLiteral handles plain values, whose legality is a pure
// function of the field type. The rendered SQL is built here so
// codegen never re-derives quoting rules.
func (a *analyzer) defaultLiteral(m *Model, f *Field, value ast.Expr) *Default {
	file := m.File
	if f.List {
		a.errorf(file, spanOf(value), "@default on a list field supports only dbgenerated(...)")
		return nil
	}
	switch f.Type.Kind {
	case TypeJson:
		a.errorf(file, spanOf(value), "@default on a Json field supports only dbgenerated(...)")
		return nil
	case TypeDateTime:
		a.errorf(file, spanOf(value), "a DateTime default must be now() or dbgenerated(...)")
		return nil
	case TypeUuid:
		a.errorf(file, spanOf(value), "a Uuid default must be uuid(), go(...), or dbgenerated(...)")
		return nil
	}
	switch value := value.(type) {
	case *ast.IntLit:
		switch f.Type.Kind {
		case TypeInt, TypeBigInt, TypeInt32:
			bits := 64
			if f.Type.Kind == TypeInt32 {
				bits = 32
			}
			if _, err := strconv.ParseInt(value.Text, 10, bits); err != nil {
				a.errorf(file, value.Span, "integer default %s overflows %s", value.Text, f.Type.Kind)
				return nil
			}
			return &Default{Kind: DefaultLiteral, SQL: value.Text}
		case TypeFloat, TypeDouble, TypeDecimal:
			return &Default{Kind: DefaultLiteral, SQL: value.Text}
		}
		a.errorf(file, value.Span, "default %s does not fit type %s", value.Text, typeDisplay(f))
	case *ast.FloatLit:
		switch f.Type.Kind {
		case TypeFloat, TypeDouble, TypeDecimal:
			return &Default{Kind: DefaultLiteral, SQL: value.Text}
		}
		a.errorf(file, value.Span, "default %s does not fit type %s", value.Text, typeDisplay(f))
	case *ast.StringLit:
		switch f.Type.Kind {
		case TypeString:
			return &Default{Kind: DefaultLiteral, SQL: sqlQuote(value.Value)}
		case TypeEnum:
			a.errorf(file, value.Span, "write the enum member bare: @default(%s)", value.Value)
			return nil
		}
		a.errorf(file, value.Span, "default %q does not fit type %s", value.Value, typeDisplay(f))
	case *ast.BoolLit:
		if f.Type.Kind == TypeBoolean {
			if value.Value {
				return &Default{Kind: DefaultLiteral, SQL: "TRUE"}
			}
			return &Default{Kind: DefaultLiteral, SQL: "FALSE"}
		}
		a.errorf(file, spanOf(value), "default %t does not fit type %s", value.Value, typeDisplay(f))
	}
	return nil
}

// typeDisplay names a field's type the way the schema spells it: the
// enum's own name for enum fields, the DSL type name otherwise.
func typeDisplay(f *Field) string {
	if f.Type.Kind == TypeEnum {
		return f.Type.Enum.Name
	}
	return f.Type.Kind.String()
}

// sqlQuote renders a single quoted SQL string literal, doubling
// embedded quotes, the one escaping rule Postgres needs.
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
