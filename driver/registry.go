package driver

import (
	"fmt"
	"strings"

	"github.com/tork-go/orm"
)

// Drivers register themselves with orm.Register, since connecting is what an
// application does and orm is the package it already imports. A migration
// needs more than a query does, though: the whole Dialect, with its DDL
// rendering and its history table.
//
// Rather than a second registry that could disagree with the first, the
// lookups here read orm's and narrow the result. A driver that implements
// Dialect is returned; one that only knows how to query is reported as such,
// which is a real possibility worth naming rather than a nil to trip over.

// Lookup returns the Dialect registered under name.
func Lookup(name string) (Dialect, bool) {
	d, ok := orm.LookupDriver(name)
	if !ok {
		return nil, false
	}
	dialect, ok := d.(Dialect)
	return dialect, ok
}

// For returns the Dialect a connection string's scheme names.
//
//	dialect, err := driver.For("postgres://tork@localhost/app")
//
// It is what lets a migration take a connection string and nothing else: the
// string already says which database it is for, so asking the caller to say it
// again in a type is asking them to repeat themselves and to be wrong
// occasionally.
func For(connString string) (Dialect, error) {
	scheme, _, ok := strings.Cut(connString, "://")
	if !ok || scheme == "" {
		return nil, fmt.Errorf("migrate: cannot tell which database %q is for; "+
			"a connection string starts with a scheme, as postgres://…", connString)
	}
	dialect, ok := Lookup(scheme)
	if ok {
		return dialect, nil
	}
	if _, registered := orm.LookupDriver(scheme); registered {
		return nil, fmt.Errorf("migrate: the %q driver can run queries but not "+
			"migrations; it implements no schema rendering", scheme)
	}
	available := orm.Drivers()
	if len(available) == 0 {
		return nil, fmt.Errorf("migrate: no driver named %q, and none are registered "+
			"at all; add the blank import that links one in, as\n"+
			"\t_ \"github.com/tork-go/orm/driver/%s\"", scheme, scheme)
	}
	return nil, fmt.Errorf("migrate: no driver named %q; registered: %s. "+
		"If %[1]q is the one you want, add\n\t_ \"github.com/tork-go/orm/driver/%[1]s\"",
		scheme, strings.Join(available, ", "))
}
