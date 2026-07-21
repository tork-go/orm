package orm

import (
	"context"
	"fmt"
	"reflect"
)

// The interfaces below are lifecycle hooks. A row type implements the ones
// it wants and the ORM calls them around the matching operation:
//
//	func (p *Post) BeforeCreate(ctx context.Context) error {
//	    p.Slug = slugify(p.Title)
//	    return nil
//	}
//
// They are declared on the row type rather than on the model because the
// row is the thing being written, and the model has no per row state to
// hook onto. That also means the receiver must be a pointer, or a mutation
// is made to a copy and thrown away; DefineTable rejects a value receiver
// rather than let that happen silently.
//
// A Before hook returning an error stops the operation before any SQL
// runs, and the error reaches the caller naming the hook it came from.
//
// Save has no hook of its own. It dispatches to an insert or an update and
// fires that operation's pair, so a row is never told about two things
// happening when only one did.
//
// The set operations that arrive with a filter fire nothing at all. They
// never load a row, so there is nothing to call a method on. That is the
// clearest reason for the split between Query and Filtered: hooks belong
// to the operations on Query, which act on a row you are holding.

// BeforeCreater is called before a row is inserted.
type BeforeCreater interface {
	BeforeCreate(ctx context.Context) error
}

// AfterCreater is called after a row is inserted, once any value the
// database generated has been read back.
type AfterCreater interface {
	AfterCreate(ctx context.Context) error
}

// BeforeUpdater is called before a row is updated.
type BeforeUpdater interface {
	BeforeUpdate(ctx context.Context) error
}

// AfterUpdater is called after a row is updated.
type AfterUpdater interface {
	AfterUpdate(ctx context.Context) error
}

// BeforeDeleter is called before a row is deleted.
type BeforeDeleter interface {
	BeforeDelete(ctx context.Context) error
}

// AfterDeleter is called after a row is deleted.
type AfterDeleter interface {
	AfterDelete(ctx context.Context) error
}

// AfterLoader is called on each row a query returns, once it has been
// scanned.
type AfterLoader interface {
	AfterLoad(ctx context.Context) error
}

// runHook calls one hook if the row implements it.
//
// The method is passed as a method expression, so H is inferred from it
// and the seven hooks share one implementation rather than repeating the
// same assert, call and wrap seven times.
func runHook[H any](ctx context.Context, table, name string, row any, call func(H, context.Context) error) error {
	h, ok := row.(H)
	if !ok {
		return nil
	}
	if err := call(h, ctx); err != nil {
		return fmt.Errorf("orm: table %q: %s: %w", table, name, err)
	}
	return nil
}

// hookInterfaces pairs each hook's name with its type, for the check that
// none of them was declared on a value receiver.
var hookInterfaces = []struct {
	name string
	typ  reflect.Type
}{
	{"BeforeCreate", reflect.TypeFor[BeforeCreater]()},
	{"AfterCreate", reflect.TypeFor[AfterCreater]()},
	{"BeforeUpdate", reflect.TypeFor[BeforeUpdater]()},
	{"AfterUpdate", reflect.TypeFor[AfterUpdater]()},
	{"BeforeDelete", reflect.TypeFor[BeforeDeleter]()},
	{"AfterDelete", reflect.TypeFor[AfterDeleter]()},
	{"AfterLoad", reflect.TypeFor[AfterLoader]()},
}

// checkHookReceivers rejects a hook declared on a value receiver.
//
// Such a hook still runs: a *E satisfies an interface a E satisfies, so
// the assertion succeeds and the method is called. But it is called on a
// copy, so anything it assigns is discarded the moment it returns, and the
// row is written exactly as it arrived. A slug computed in BeforeCreate
// would simply not be there, with nothing anywhere reporting why.
//
// That is the same class of mistake DefineTable already panics for, and it
// is caught the same way: at declaration, in the source, rather than on
// the first row.
func checkHookReceivers(table string, entity reflect.Type) error {
	for _, h := range hookInterfaces {
		if entity.Implements(h.typ) {
			return fmt.Errorf("orm: table %q: %s is declared on %s rather than on *%s, "+
				"so it would run against a copy and anything it changed would be "+
				"discarded; give it a pointer receiver",
				table, h.name, entity, entity)
		}
	}
	return nil
}
