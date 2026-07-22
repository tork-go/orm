package orm

import (
	"fmt"
	"strings"
)

// Scoper is implemented by a model that filters every read and set
// operation on its table by default, unless the query calls Unscoped().
//
//	func (m *PostModel) DefaultScope() orm.Predicate {
//	    return m.Published.Eq(true)
//	}
//
// It is asserted in DefineTable and the model itself is stashed on the
// table's state rather than called there, for the same reason Relater is:
// DefaultScope commonly reaches for another table's column, and package
// level variable initialisation order may not have reached that table yet.
type Scoper interface {
	DefaultScope() Predicate
}

// defaultScope returns the table's implicit filter: the model's Scoper
// predicate ANDed with its soft-delete "not yet deleted" filter, when
// either or both are declared. nil when neither is.
//
// Resolved once and cached, for the same reason relationship info is:
// nothing here changes after DefineTable returns, so there is no reason to
// rebuild it on every query.
func (st *tableState) defaultScope() Predicate {
	st.scopeOnce.Do(func() {
		var parts []Predicate
		if st.scoper != nil {
			if p := st.scoper.DefaultScope(); p != nil {
				parts = append(parts, p)
			}
		}
		if st.softDelete != nil {
			parts = append(parts, Nullness{Col: st.softDelete})
		}
		if len(parts) > 0 {
			st.scopeVal = And(parts...)
		}
	})
	return st.scopeVal
}

// softDeleteColumnOf returns the table's soft-delete marker column, or nil
// if it declares none, erroring if it declares more than one: only one
// column can be stamped or checked, so two is ambiguous rather than a
// choice DefineTable could make on the caller's behalf.
func softDeleteColumnOf(table string, cols []ColumnMeta) (ColumnMeta, error) {
	var found []ColumnMeta
	for _, c := range cols {
		if c.IsSoftDeleteColumn() {
			found = append(found, c)
		}
	}
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return found[0], nil
	default:
		names := make([]string, len(found))
		for i, c := range found {
			names[i] = `"` + c.Name() + `"`
		}
		return nil, fmt.Errorf("orm: table %q: declares %d soft-delete columns (%s); "+
			"only one is allowed", table, len(found), strings.Join(names, ", "))
	}
}
