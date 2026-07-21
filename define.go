package orm

import (
	"fmt"
	"reflect"
)

// ownerSetter is the write side of ColumnMeta.OwnerTable. It is unexported
// and satisfied by every column type through Column[T], which is what lets
// DefineTable bind a column it holds only as a ColumnMeta without widening
// the public interface with a setter callers have no business calling.
type ownerSetter interface {
	setOwner(name string)
}

// TableBuilder creates a table's columns. DefineTable hands one to the
// function that declares a model, and every column made through it belongs
// to that table:
//
//	Username: t.String("username").Unique().NotNull().MaxLen(30),
//
// The methods are named for the column kind rather than the Go type, so
// what is available on the result is visible from the call: t.String gives
// a StringColumn, which has Contains and MaxLen, while t.Int gives an
// IntColumn, which has neither.
//
// A builder holds no state of its own beyond the table it is filling.
// Columns are collected afterwards by walking the returned model's fields,
// so a column the builder made but the model never stored is simply not
// part of the table. There is no way to accidentally register one twice
// or to register one under the wrong name.
type TableBuilder[E any] struct {
	st *tableState
}

// Table returns the table identity to embed in the model being built.
//
// It is safe to call before any column exists: the Table shares the
// builder's state by pointer, so the columns declared alongside it in the
// same composite literal are visible through it afterwards regardless of
// the order Go evaluates those fields in.
func (b *TableBuilder[E]) Table() Table[E] {
	return Table[E]{st: b.st}
}

// Bool declares a non-nullable bool column.
func (b *TableBuilder[E]) Bool(name string) *BoolColumn { return NewBoolColumn(name) }

// NullableBool declares a nullable bool column.
func (b *TableBuilder[E]) NullableBool(name string) *NullableBoolColumn {
	return NewNullableBoolColumn(name)
}

// Int declares a non-nullable int column.
func (b *TableBuilder[E]) Int(name string) *IntColumn { return NewIntColumn(name) }

// NullableInt declares a nullable int column.
func (b *TableBuilder[E]) NullableInt(name string) *NullableIntColumn {
	return NewNullableIntColumn(name)
}

// Int32 declares a non-nullable int32 column.
func (b *TableBuilder[E]) Int32(name string) *Int32Column { return NewInt32Column(name) }

// NullableInt32 declares a nullable int32 column.
func (b *TableBuilder[E]) NullableInt32(name string) *NullableInt32Column {
	return NewNullableInt32Column(name)
}

// BigInt declares a non-nullable int64 column.
func (b *TableBuilder[E]) BigInt(name string) *BigIntColumn { return NewBigIntColumn(name) }

// NullableBigInt declares a nullable int64 column.
func (b *TableBuilder[E]) NullableBigInt(name string) *NullableBigIntColumn {
	return NewNullableBigIntColumn(name)
}

// Float declares a non-nullable float32 column (REAL). For float64, use
// Double.
func (b *TableBuilder[E]) Float(name string) *FloatColumn { return NewFloatColumn(name) }

// NullableFloat declares a nullable float32 column.
func (b *TableBuilder[E]) NullableFloat(name string) *NullableFloatColumn {
	return NewNullableFloatColumn(name)
}

// Double declares a non-nullable float64 column (DOUBLE PRECISION).
func (b *TableBuilder[E]) Double(name string) *DoubleColumn { return NewDoubleColumn(name) }

// NullableDouble declares a nullable float64 column.
func (b *TableBuilder[E]) NullableDouble(name string) *NullableDoubleColumn {
	return NewNullableDoubleColumn(name)
}

// Decimal declares a non-nullable fixed-point column.
func (b *TableBuilder[E]) Decimal(name string) *DecimalColumn { return NewDecimalColumn(name) }

// NullableDecimal declares a nullable fixed-point column.
func (b *TableBuilder[E]) NullableDecimal(name string) *NullableDecimalColumn {
	return NewNullableDecimalColumn(name)
}

// String declares a non-nullable string column.
func (b *TableBuilder[E]) String(name string) *StringColumn { return NewStringColumn(name) }

// NullableString declares a nullable string column.
func (b *TableBuilder[E]) NullableString(name string) *NullableStringColumn {
	return NewNullableStringColumn(name)
}

// Time declares a non-nullable timestamp column.
func (b *TableBuilder[E]) Time(name string) *TimeColumn { return NewTimeColumn(name) }

// NullableTime declares a nullable timestamp column.
func (b *TableBuilder[E]) NullableTime(name string) *NullableTimeColumn {
	return NewNullableTimeColumn(name)
}

// Enum declares a non-nullable enum column of the database enum type
// typeName, with the given values in order.
func (b *TableBuilder[E]) Enum(name, typeName string, values ...string) *EnumColumn {
	return NewEnumColumn(name, typeName, values...)
}

// NullableEnum declares a nullable enum column of the database enum type
// typeName, with the given values in order.
func (b *TableBuilder[E]) NullableEnum(name, typeName string, values ...string) *NullableEnumColumn {
	return NewNullableEnumColumn(name, typeName, values...)
}

// UUID declares a non-nullable UUID column.
func (b *TableBuilder[E]) UUID(name string) *UUIDColumn { return NewUUIDColumn(name) }

// NullableUUID declares a nullable UUID column.
func (b *TableBuilder[E]) NullableUUID(name string) *NullableUUIDColumn {
	return NewNullableUUIDColumn(name)
}

// JSON and array columns have no builder method here. Their element or
// document type is a type parameter, and Go does not allow a method to
// declare one, so they are built with the package-level NewJSONColumn and
// NewArrayColumn inside the same function, and with the same result:
// DefineTable binds every column it finds on the returned model, however
// that column was constructed.
//
//	Prefs: orm.NewJSONColumn[Preferences]("prefs"),
//	Tags:  orm.NewArrayColumn[string]("tags"),

// DefineTable declares the model for the table named name, whose rows scan
// into E.
//
//	type User struct {
//	    ID       int
//	    Username string
//	    Email    *string
//	}
//
//	type UserModel struct {
//	    orm.Table[User]
//	    ID       *orm.IntColumn
//	    Username *orm.StringColumn
//	    Email    *orm.NullableStringColumn
//	}
//
//	var Users = orm.DefineTable[User]("users", func(t *orm.TableBuilder[User]) *UserModel {
//	    return &UserModel{
//	        Table:    t.Table(),
//	        ID:       t.Int("id").PrimaryKey(),
//	        Username: t.String("username").Unique().NotNull().MaxLen(30),
//	        Email:    t.NullableString("email"),
//	    }
//	})
//
// Only E is written explicitly; the model type is inferred from what the
// function returns, so it may be a pointer or a value as the caller
// prefers.
//
// Declaring a model this way does three things a hand-built one does not.
// Every column is bound to the table, so OwnerTable reports it and
// generated SQL can qualify names. The columns are recorded in struct
// field order, which fixes the order a SELECT lists them and therefore how
// a result row maps back to fields, since driver.Rows scans positionally
// and exposes no column names. And each column is matched to the field of
// E it scans into.
//
// # Matching columns to fields
//
// A column matches the field tagged `db:"<column name>"` if there is one,
// and otherwise the field whose name snake-cases to the column's name,
// so AuthorID to author_id and CreatedAt to created_at. Fields tagged `db:"-"`
// are skipped. Fields matching no column are left alone, which is what
// lets an entity carry related rows and computed values alongside its
// columns.
//
// # Failure is a panic
//
// A column with no matching field, or one whose field has the wrong type,
// panics. DefineTable is meant for a package-level var, where returning an
// error would leave every caller to check something that cannot vary at
// run time, so it follows regexp.MustCompile instead: the mistake is in
// the source, the program cannot run correctly with it, and it surfaces at
// startup rather than at the first query.
func DefineTable[E any, M Model](name string, build func(*TableBuilder[E]) M) M {
	st := &tableState{name: name, entity: reflect.TypeFor[E]()}
	m := build(&TableBuilder[E]{st: st})

	if got := m.TableName(); got != name {
		panic(fmt.Sprintf("orm: table %q: the model's Table field reports %q; "+
			"it must be set from the builder, as Table: t.Table()", name, got))
	}

	cols := Columns(m)
	st.cols = cols
	for _, c := range cols {
		if s, ok := c.(ownerSetter); ok {
			s.setOwner(name)
		}
	}

	idx, err := resolveEntityFields(name, st.entity, cols)
	if err != nil {
		panic(err.Error())
	}
	st.fieldIdx = idx

	return m
}

// entityField is a resolved field of the entity: where it sits and what
// type it holds. The index is a path rather than a single index because a
// field promoted from an embedded struct lives one or more levels down,
// which is exactly what reflect.Value.FieldByIndex walks.
type entityField struct {
	index []int
	typ   reflect.Type
	// ambiguous marks a name reachable at the same depth through two
	// different embedded structs. Go rejects such a selector only where it
	// is written, so this is reported only if a column actually wants it.
	ambiguous bool
}

// collectEntityFields walks entity breadth first and returns its fields
// keyed by db tag and by snake-cased name.
//
// Breadth first is what makes shadowing match Go's own rules: a field
// declared directly on the entity wins over one promoted from an embedded
// struct, and a shallower embedded struct wins over a deeper one. Two
// fields reachable at the same depth are ambiguous, exactly as they would
// be in a selector, and are marked rather than rejected outright since a
// name nobody references is not a problem.
//
// An embedded struct is walked whether or not its own type is exported,
// since reflect happily takes the address of an exported field reached
// through an unexported embedded one, which is all scanning needs. The
// embedded field itself is only registered as a candidate when it is
// exported, because an unexported name is not one a column could match.
//
// Embedded pointers are not walked. FieldByIndex panics rather than
// allocate when it meets a nil pointer partway down a path, so scanning
// into one would depend on the caller having filled it in first. Leaving
// it out is better than a nil dereference on the first row.
func collectEntityFields(entity reflect.Type) (byTag, byName map[string]entityField) {
	byTag = make(map[string]entityField)
	byName = make(map[string]entityField)

	type queued struct {
		typ    reflect.Type
		prefix []int
	}
	level := []queued{{typ: entity}}
	// Guards against a struct that embeds its own type transitively, which
	// would otherwise queue forever.
	seen := map[reflect.Type]bool{entity: true}

	for len(level) > 0 {
		var next []queued
		tagHere := make(map[string]entityField)
		nameHere := make(map[string]entityField)

		for _, q := range level {
			for i := range q.typ.NumField() {
				f := q.typ.Field(i)
				index := append(append([]int{}, q.prefix...), i)

				if f.Anonymous && f.Type.Kind() == reflect.Struct && !seen[f.Type] {
					seen[f.Type] = true
					next = append(next, queued{typ: f.Type, prefix: index})
				}
				if !f.IsExported() {
					continue
				}

				candidate := entityField{index: index, typ: f.Type}
				if tag, ok := f.Tag.Lookup("db"); ok {
					if tag == "-" {
						continue
					}
					record(tagHere, tag, candidate)
					continue
				}
				record(nameHere, snakeCase(f.Name), candidate)
			}
		}

		// Only names not already claimed at a shallower depth are taken,
		// which is what gives the outer declaration precedence.
		promote(byTag, tagHere)
		promote(byName, nameHere)
		level = next
	}
	return byTag, byName
}

// record adds candidate under key, marking the entry ambiguous if key is
// already taken at this depth.
func record(m map[string]entityField, key string, candidate entityField) {
	if existing, ok := m[key]; ok {
		existing.ambiguous = true
		m[key] = existing
		return
	}
	m[key] = candidate
}

// promote copies this depth's fields into the accumulated map, leaving
// anything already found at a shallower depth alone.
func promote(dst, src map[string]entityField) {
	for k, v := range src {
		if _, ok := dst[k]; !ok {
			dst[k] = v
		}
	}
}

// resolveEntityFields maps each column to the index path of the entity
// field it scans into.
//
// Index paths rather than names because that is what reflect.Value's
// FieldByIndex takes, so the per-row scan costs a slice walk instead of a
// map lookup and a string compare.
func resolveEntityFields(table string, entity reflect.Type, cols []ColumnMeta) (map[string][]int, error) {
	if entity.Kind() != reflect.Struct {
		return nil, fmt.Errorf("orm: table %q: entity type %s is not a struct", table, entity)
	}
	if entity.NumField() == 0 && len(cols) > 0 {
		return nil, fmt.Errorf("orm: table %q: entity type %s has no fields, "+
			"so none of the %d columns can be scanned; declare the row struct, "+
			"or use NewTable[NoEntity] for a model that backs no queries",
			table, entity, len(cols))
	}

	byTag, byName := collectEntityFields(entity)

	out := make(map[string][]int, len(cols))
	for _, c := range cols {
		field, ok := byTag[c.Name()]
		if !ok {
			field, ok = byName[c.Name()]
		}
		if !ok {
			return nil, fmt.Errorf("orm: table %q: column %q has no field on %s "+
				"(looked for a field whose name snake-cases to %q, or one tagged `db:%q`)",
				table, c.Name(), entity, c.Name(), c.Name())
		}
		if field.ambiguous {
			return nil, fmt.Errorf("orm: table %q: column %q is ambiguous on %s: "+
				"two embedded structs promote a field named %q at the same depth; "+
				"declare it directly on %s, or point one of them at another column "+
				"with a `db:` tag",
				table, c.Name(), entity, c.Name(), entity)
		}
		if field.typ != c.GoType() {
			return nil, fmt.Errorf("orm: table %q: column %q is %s but %s.%s is %s",
				table, c.Name(), c.GoType(), entity,
				entity.FieldByIndex(field.index).Name, field.typ)
		}
		out[c.Name()] = field.index
	}
	return out, nil
}
