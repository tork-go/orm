package orm

import (
	"fmt"
	"reflect"
)

// scanStruct reads the current row of rows into a new T, matching T's
// top-level exported fields, in declaration order, positionally against
// the row's columns.
//
// It is independent of tableState/fieldIdx on purpose: T here is never a
// declared model, just a plain struct a caller wrote for one query
// (RawQuery, SelectAs), so there is no entity mapping to reuse the way an
// ordinary row scan has one. Matching stays positional rather than by name
// or tag, the same convention every other scan path in this package
// follows, since driver.Rows exposes no column names to match by anyway.
func scanStruct[T any](rows Rows) (T, error) {
	var v T
	rv := reflect.ValueOf(&v).Elem()
	if rv.Kind() != reflect.Struct {
		return v, fmt.Errorf("orm: %s is not a struct", rv.Type())
	}
	var dests []any
	for i := 0; i < rv.NumField(); i++ {
		if !rv.Type().Field(i).IsExported() {
			continue
		}
		dests = append(dests, rv.Field(i).Addr().Interface())
	}
	if err := rows.Scan(dests...); err != nil {
		return v, fmt.Errorf("scanning row: %w", err)
	}
	return v, nil
}
