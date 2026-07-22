package orm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
)

// Cursor is an opaque bookmark into an ordered query: the ordering it was
// taken under, and the values of those columns for one row, encoded so a
// caller can hand it to a client and receive it back to resume after that
// row with After.
//
// It is parameterised by the row type, so a Cursor[Post] cannot be handed
// to a query over User: the compiler catches that, rather than the
// database receiving a seek condition naming columns that belong to
// another table.
//
// Keyset pagination needs the ordering to determine a row's position
// uniquely, which means it should end in a column no two rows share,
// usually the primary key. An ordering that does not risks skipping or
// repeating rows across pages when ties exist; Cursor and After do not
// check for this, since nothing at this layer knows whether a composite
// unique constraint makes an ordering safe.
type Cursor[E any] struct {
	table string
	terms []cursorTerm
}

// cursorTerm is one column's contribution to a Cursor: which column, which
// direction it orders by, and the row's value there, pre-marshalled so
// decoding can target the consuming query's own ordering column's Go type
// rather than whatever type the cursor happened to be built with.
type cursorTerm struct {
	Col   string          `json:"c"`
	Desc  bool            `json:"d"`
	Value json.RawMessage `json:"v"`
}

// cursorWire is what String and ParseCursor actually encode: a Cursor's
// terms plus the table it was taken from, so a token built for the wrong
// table is reported rather than silently seeking on columns that happen to
// share its names.
type cursorWire struct {
	Table string       `json:"t"`
	Terms []cursorTerm `json:"o"`
}

// Cursor captures row's position in this query's current ordering, so a
// later query can resume after it with After.
//
//	page, err := Users.With(db).OrderBy(Users.ID.Asc()).Limit(20).All(ctx)
//	cursor, err := Users.With(db).OrderBy(Users.ID.Asc()).Cursor(page[len(page)-1])
//	next, err := Users.With(db).OrderBy(Users.ID.Asc()).After(cursor).Limit(20).All(ctx)
//
// It needs an OrderBy to capture a position in. If the query narrowed which
// columns it reads with Select, every ordering column must be among them: a
// column that was not read carries its zero value, not the row's actual
// position, and a cursor built from it would seek from the wrong place.
func (f *Filtered[E]) Cursor(row *E) (Cursor[E], error) {
	if err := f.readyToRead(); err != nil {
		return Cursor[E]{}, err
	}
	if len(f.ords) == 0 {
		return Cursor[E]{}, fmt.Errorf("orm: table %q: Cursor needs an OrderBy to capture "+
			"a position in", f.st.name)
	}
	if row == nil {
		return Cursor[E]{}, fmt.Errorf("orm: table %q: Cursor was given a nil row", f.st.name)
	}
	// A cursor seeks by comparing the ordering's own columns against the
	// values it captured out of a row. A computed ordering has no field to
	// read one from, so there is nothing to capture and nothing to compare
	// against; saying so beats seeking from a zero value that would silently
	// page from the wrong place.
	for i, o := range f.ords {
		if o.expr != nil {
			return Cursor[E]{}, fmt.Errorf("orm: table %q: Cursor cannot page an ordering "+
				"computed by the database, at position %d; order by a column, or page with "+
				"Limit and Offset instead", f.st.name, i)
		}
	}
	if f.sel != nil {
		read := make(map[string]bool, len(f.sel))
		for _, c := range f.sel {
			read[c.Name()] = true
		}
		for _, o := range f.ords {
			if !read[o.Col.Name()] {
				return Cursor[E]{}, fmt.Errorf("orm: table %q: Cursor needs column %q, "+
					"which this query does not select; add it to the Select, or drop the "+
					"Select", f.st.name, o.Col.Name())
			}
		}
	}

	val := reflect.ValueOf(row).Elem()
	terms := make([]cursorTerm, len(f.ords))
	for i, o := range f.ords {
		index, ok := f.st.fieldIdx[o.Col.Name()]
		if !ok {
			return Cursor[E]{}, fmt.Errorf("orm: table %q: column %q has no mapped field",
				f.st.name, o.Col.Name())
		}
		v := fieldByIndexAlloc(val, index).Interface()
		// Every column's Go type is a scalar or a pointer to one (see
		// column.go and column_misc.go), none of which encoding/json can
		// fail to marshal, so this error is unreachable in practice; it is
		// still checked, since a future column kind is not guaranteed to
		// stay that way.
		b, err := json.Marshal(v)
		if err != nil {
			return Cursor[E]{}, fmt.Errorf("orm: table %q: encoding cursor column %q: %w",
				f.st.name, o.Col.Name(), err)
		}
		terms[i] = cursorTerm{Col: o.Col.Name(), Desc: o.Desc, Value: b}
	}
	return Cursor[E]{table: f.st.name, terms: terms}, nil
}

// Cursor is Filtered.Cursor, off an unfiltered query.
func (q *Query[E]) Cursor(row *E) (Cursor[E], error) { return q.filtered().Cursor(row) }

// String encodes the cursor as an opaque token, safe to hand to a client
// and read back with ParseCursor.
//
// Every field a Cursor carries is a string, a bool or already-valid JSON,
// none of which encoding/json can fail on, so this needs no error return;
// MarshalText below is the form that satisfies encoding.TextMarshaler.
func (c Cursor[E]) String() string {
	wire := cursorWire{Table: c.table, Terms: c.terms}
	b, _ := json.Marshal(wire)
	out := make([]byte, base64.RawURLEncoding.EncodedLen(len(b)))
	base64.RawURLEncoding.Encode(out, b)
	return string(out)
}

// MarshalText implements encoding.TextMarshaler, encoding the cursor the
// same way String does.
func (c Cursor[E]) MarshalText() ([]byte, error) {
	return []byte(c.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, decoding a token
// String or MarshalText produced.
func (c *Cursor[E]) UnmarshalText(text []byte) error {
	decoded := make([]byte, base64.RawURLEncoding.DecodedLen(len(text)))
	n, err := base64.RawURLEncoding.Decode(decoded, text)
	if err != nil {
		return fmt.Errorf("orm: cursor is not validly encoded: %w", err)
	}
	var wire cursorWire
	if err := json.Unmarshal(decoded[:n], &wire); err != nil {
		return fmt.Errorf("orm: cursor is not validly encoded: %w", err)
	}
	c.table = wire.Table
	c.terms = wire.Terms
	return nil
}

// ParseCursor decodes a token String or MarshalText produced. The row type
// is named explicitly, the same way Select's column is, so a mismatch
// between the cursor and the query it is handed to is caught by the
// compiler wherever possible and reported by After otherwise, rather than
// silently seeking on the wrong table's columns.
func ParseCursor[E any](s string) (Cursor[E], error) {
	var c Cursor[E]
	if err := c.UnmarshalText([]byte(s)); err != nil {
		return Cursor[E]{}, err
	}
	return c, nil
}

// After narrows to rows that come after cursor in this query's ordering.
//
//	next, err := Users.With(db).OrderBy(Users.ID.Asc()).After(cursor).Limit(20).All(ctx)
//
// The cursor must have been taken under the same ordering this query
// carries, in both which columns and which direction: one built for
// OrderBy(CreatedAt.Desc(), ID.Asc()) cannot resume a query ordered only by
// ID, and After reports that rather than silently seeking on the wrong
// columns.
//
// It is rejected on UpdateAll and DeleteAll the same way OrderBy itself is:
// After needs an OrderBy to mean anything, and readyForSetOp already
// refuses one there.
func (f *Filtered[E]) After(cursor Cursor[E]) *Filtered[E] {
	out := f.clone()
	if len(out.ords) == 0 {
		out.fail(fmt.Errorf("orm: table %q: After needs an OrderBy to seek within",
			out.tableName()))
		return out
	}
	if cursor.table != "" && cursor.table != out.st.name {
		out.fail(fmt.Errorf("orm: table %q: After was given a cursor taken from table %q",
			out.st.name, cursor.table))
		return out
	}
	if len(cursor.terms) != len(out.ords) {
		out.fail(fmt.Errorf("orm: table %q: After was given a cursor with %d ordering "+
			"columns, but this query orders by %d", out.st.name, len(cursor.terms), len(out.ords)))
		return out
	}

	values := make([]any, len(out.ords))
	for i, o := range out.ords {
		term := cursor.terms[i]
		// Cursor refuses to build one over a computed ordering, so a cursor
		// in hand never names one; a query that grew an expression ordering
		// since is what reaches here, and it has no column to match against.
		if o.expr != nil {
			out.fail(fmt.Errorf("orm: table %q: After cannot seek within an ordering "+
				"computed by the database, at position %d; order by a column, or page "+
				"with Limit and Offset instead", out.st.name, i))
			return out
		}
		if term.Col != o.Col.Name() || term.Desc != o.Desc {
			out.fail(fmt.Errorf("orm: table %q: After's cursor orders by %q at position %d, "+
				"but this query orders by %q there", out.st.name, term.Col, i, o.Col.Name()))
			return out
		}
		dest := reflect.New(o.Col.GoType())
		if err := json.Unmarshal(term.Value, dest.Interface()); err != nil {
			out.fail(fmt.Errorf("orm: table %q: decoding cursor column %q: %w",
				out.st.name, term.Col, err))
			return out
		}
		values[i] = dest.Elem().Interface()
	}
	out.preds = append(out.preds, seekPredicate(out.ords, values))
	return out
}

// After is Filtered.After, off an unfiltered query.
func (q *Query[E]) After(cursor Cursor[E]) *Filtered[E] { return q.filtered().After(cursor) }

// seekPredicate builds the keyset "after this row" condition for an
// ordering of N columns: an OR of N branches, the k-th branch equating
// every earlier column and strictly comparing the k-th in the direction
// that means "later in this order" (greater than for ascending, less than
// for descending).
//
// It is built entirely from Comparison, And and Or, the same predicates any
// caller's own Where produces, so the compiler needs nothing new to render
// it and it composes with everything a predicate already composes with.
func seekPredicate(ords []Ordering, values []any) Predicate {
	terms := make([]Predicate, len(ords))
	for k := range ords {
		conj := make([]Predicate, 0, k+1)
		for j := 0; j < k; j++ {
			conj = append(conj, Comparison{Col: ords[j].Col, Op: OpEquals, Value: values[j]})
		}
		op := OpGreaterThan
		if ords[k].Desc {
			op = OpLessThan
		}
		conj = append(conj, Comparison{Col: ords[k].Col, Op: op, Value: values[k]})
		terms[k] = And(conj...)
	}
	return Or(terms...)
}
