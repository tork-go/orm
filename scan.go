package orm

import (
	"fmt"
	"reflect"
)

// Columns returns the table's columns in the order a generated SELECT
// lists them, which is the order they were declared on the model.
//
// That order is load-bearing rather than cosmetic. Rows exposes no column
// names, so scanning is positional: the only thing tying a value in the
// result back to a field is that both sides walk this same list.
func (t Table[E]) Columns() []ColumnMeta {
	if t.st == nil {
		return nil
	}
	out := make([]ColumnMeta, len(t.st.cols))
	copy(out, t.st.cols)
	return out
}

// ScanRow reads the row rs is currently positioned on into a new E.
//
// The caller advances rs; this reads whatever row Next last moved to, so a
// loop over a result set stays the caller's to write. Destinations come
// from the entity mapping DefineTable resolved, so a field promoted from
// an embedded struct is filled in exactly like a direct one.
//
// Document columns cannot be scanned into their Go type directly, since
// the database hands back encoded bytes. They are staged through a []byte
// and then decoded with the column's own codec, which is what makes a
// custom Serialize pair apply on the way in as well as on the way out.
func (t Table[E]) ScanRow(rs Rows) (E, error) {
	var e E
	if t.st == nil || t.st.fieldIdx == nil {
		return e, fmt.Errorf("orm: table %q: no entity mapping, so rows cannot be "+
			"scanned; declare the model with DefineTable rather than NewTable",
			t.TableName())
	}

	v := reflect.ValueOf(&e).Elem()
	dests := make([]any, len(t.st.cols))

	// staged records which positions hold encoded bytes rather than a
	// final value, so they can be decoded once the scan has filled them.
	type stagedField struct {
		buf   *[]byte
		field reflect.Value
		col   ColumnMeta
	}
	var staged []stagedField

	for i, c := range t.st.cols {
		field := v.FieldByIndex(t.st.fieldIdx[c.Name()])
		if isDocumentColumn(c) {
			buf := new([]byte)
			dests[i] = buf
			staged = append(staged, stagedField{buf: buf, field: field, col: c})
			continue
		}
		dests[i] = field.Addr().Interface()
	}

	if err := rs.Scan(dests...); err != nil {
		return e, fmt.Errorf("orm: table %q: scanning row: %w", t.TableName(), err)
	}

	for _, s := range staged {
		// A NULL document leaves the field at its zero value, which for a
		// nullable column is the nil pointer that NULL means.
		if *s.buf == nil {
			continue
		}
		codec, ok := s.col.(ValueCodec)
		if !ok {
			return e, fmt.Errorf("orm: table %q: column %q cannot decode its value",
				t.TableName(), s.col.Name())
		}
		decoded, err := codec.UnmarshalAny(*s.buf)
		if err != nil {
			return e, fmt.Errorf("orm: table %q: %w", t.TableName(), err)
		}
		dv := reflect.ValueOf(decoded)
		if !dv.IsValid() {
			continue
		}
		if !dv.Type().AssignableTo(s.field.Type()) {
			return e, fmt.Errorf("orm: table %q: column %q decoded to %s, want %s",
				t.TableName(), s.col.Name(), dv.Type(), s.field.Type())
		}
		s.field.Set(dv)
	}

	return e, nil
}

// isDocumentColumn reports whether a column's value travels as encoded
// bytes. IsJSONB already covers a column that only called Serialize, so
// these two questions between them catch every encoded column.
func isDocumentColumn(c ColumnMeta) bool {
	return c.IsJSON() || c.IsJSONB()
}
