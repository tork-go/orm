package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tork-go/orm/gen/analyze"
)

// scalarSpec ties one DSL scalar to everything its emission needs: the
// Go type of the row struct field, the stem shared by the ORM's column
// type and builder names (Int for IntColumn and t.Int), and the import
// the Go type drags in.
type scalarSpec struct {
	goType     string
	stem       string
	importPath string
}

var scalars = map[analyze.TypeKind]scalarSpec{
	analyze.TypeBoolean:  {"bool", "Bool", ""},
	analyze.TypeInt:      {"int", "Int", ""},
	analyze.TypeInt32:    {"int32", "Int32", ""},
	analyze.TypeBigInt:   {"int64", "BigInt", ""},
	analyze.TypeFloat:    {"float32", "Float", ""},
	analyze.TypeDouble:   {"float64", "Double", ""},
	analyze.TypeDecimal:  {"decimal.Decimal", "Decimal", "github.com/shopspring/decimal"},
	analyze.TypeString:   {"string", "String", ""},
	analyze.TypeDateTime: {"time.Time", "Time", "time"},
	analyze.TypeUuid:     {"uuid.UUID", "UUID", "github.com/google/uuid"},
}

// varName is the package variable a model declares, named after its
// table: users becomes Users, blog_posts becomes BlogPosts. The
// analyzer already guaranteed these do not collide with anything else
// the package generates.
func varName(m *analyze.Model) string {
	return analyze.GoName(m.TableName)
}

// rowType renders a field's type in the row struct: the base Go type,
// wrapped for optional and list the same way the ORM's column types
// expect to scan them.
func (g *generator) rowType(imp *imports, f *analyze.Field) string {
	var base string
	switch f.Type.Kind {
	case analyze.TypeEnum:
		base = "string"
	case analyze.TypeJson:
		base = g.qualify(imp, f.GoType)
	default:
		spec := scalars[f.Type.Kind]
		if spec.importPath != "" {
			imp.add(spec.importPath)
		}
		base = spec.goType
	}
	switch {
	case f.List && f.Optional:
		return "*[]" + base
	case f.List:
		return "[]" + base
	case f.Optional:
		return "*" + base
	default:
		return base
	}
}

// columnType renders a field's column declaration in the model struct,
// such as *orm.NullableStringArrayColumn or *orm.JSONColumn[Profile].
func (g *generator) columnType(imp *imports, f *analyze.Field) string {
	if f.Type.Kind == analyze.TypeJson {
		q := g.qualify(imp, f.GoType)
		if f.Optional {
			return "*orm.NullableJSONColumn[" + q + "]"
		}
		return "*orm.JSONColumn[" + q + "]"
	}
	stem := "Enum"
	if f.Type.Kind != analyze.TypeEnum {
		stem = scalars[f.Type.Kind].stem
	}
	name := stem
	if f.List {
		name += "Array"
	}
	if f.Optional {
		name = "Nullable" + name
	}
	return "*orm." + name + "Column"
}

// constructor renders the builder call that creates a field's column.
func (g *generator) constructor(imp *imports, f *analyze.Field) string {
	col := strconv.Quote(f.ColumnName)
	switch f.Type.Kind {
	case analyze.TypeJson:
		q := g.qualify(imp, f.GoType)
		if f.Optional {
			return fmt.Sprintf("orm.NewNullableJSONColumn[%s](%s)", q, col)
		}
		return fmt.Sprintf("orm.NewJSONColumn[%s](%s)", q, col)
	case analyze.TypeEnum:
		args := []string{col, strconv.Quote(f.Type.Enum.DBName)}
		for _, v := range f.Type.Enum.Values {
			args = append(args, strconv.Quote(v))
		}
		name := "Enum"
		if f.Optional {
			name = "NullableEnum"
		}
		return "t." + name + "(" + strings.Join(args, ", ") + ")"
	}
	name := scalars[f.Type.Kind].stem
	if f.List {
		name += "Array"
	}
	if f.Optional {
		name = "Nullable" + name
	}
	return "t." + name + "(" + col + ")"
}

// markerType renders a relation field's marker in the model struct.
func markerType(f *analyze.Field) string {
	target := f.Relation.Target.Name
	switch f.Relation.Kind {
	case analyze.RelBelongsTo:
		return "orm.BelongsTo[" + target + "]"
	case analyze.RelHasOne:
		return "orm.HasOne[" + target + "]"
	case analyze.RelHasMany:
		return "orm.HasMany[" + target + "]"
	default:
		return "orm.ManyToMany[" + target + "]"
	}
}

// rowRelationType renders a relation field in the row struct: a slice
// for the many kinds, a pointer for the single ones, matching what the
// ORM's Load fills.
func rowRelationType(f *analyze.Field) string {
	target := f.Relation.Target.Name
	switch f.Relation.Kind {
	case analyze.RelHasMany, analyze.RelManyToMany:
		return "[]" + target
	default:
		return "*" + target
	}
}

// actionExpr renders a referential action, or empty for the ones that
// need no call: absent, and the explicit NoAction that is already the
// ORM's zero value.
func actionExpr(action string) string {
	switch action {
	case "", "NoAction":
		return ""
	default:
		return "orm.Action" + action
	}
}
