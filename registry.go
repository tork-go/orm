package orm

import (
	"reflect"
	"sync"
)

// registry maps a row type to the table declared for it.
//
// Relationships are the reason it exists. HasMany[Post] names the related
// row type and nothing else, so resolving it means finding the table that
// scans into a Post, which no reference from the declaring model could
// supply: pointing at the other table directly would make the two models
// depend on each other, and Go rejects a cycle between package level
// variables outright.
//
// Lookups are therefore deferred to first use rather than done while
// tables are being declared. Package level variables initialise in
// dependency order within a file set, so a table may well not be
// registered yet at the moment another one mentions its row type.
var registry = struct {
	sync.RWMutex
	byEntity map[reflect.Type]*tableState
}{byEntity: map[reflect.Type]*tableState{}}

// registerTable records st under its row type.
//
// A row type declared twice replaces the earlier entry rather than
// erroring. Tests routinely declare throwaway tables, and a duplicate row
// type is a mistake that shows up plainly the first time a relationship
// resolves to the wrong table, whereas panicking here would make an
// unrelated test order dependent.
func registerTable(entity reflect.Type, st *tableState) {
	registry.Lock()
	defer registry.Unlock()
	registry.byEntity[entity] = st
}

// lookupTable returns the table declared for the given row type.
func lookupTable(entity reflect.Type) (*tableState, bool) {
	registry.RLock()
	defer registry.RUnlock()
	st, ok := registry.byEntity[entity]
	return st, ok
}
