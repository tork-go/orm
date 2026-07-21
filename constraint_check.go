package orm

// CheckDef declares one table-level CHECK constraint. Expression is a raw
// SQL boolean expression Tork does not parse or validate, the same spirit
// as ServerDefault's raw expression. See Checker.
type CheckDef struct {
	name       string
	expression string
}

// NewCheckDef declares a CHECK constraint with the given raw SQL boolean
// expression. Name is optional: leave it unset, or call Named to
// override, to have schema.ExtractSchema auto-generate one.
func NewCheckDef(expression string) CheckDef {
	return CheckDef{expression: expression}
}

// Named overrides the auto-generated name. CheckDef is returned by value,
// the same reasoning as IndexDef: it's never a struct field walked by
// reflection.
func (d CheckDef) Named(name string) CheckDef {
	d.name = name
	return d
}

// Name returns the name passed to Named, or "" if it was never called.
func (d CheckDef) Name() string { return d.name }

// Expression returns the raw SQL expression passed to NewCheckDef.
func (d CheckDef) Expression() string { return d.expression }

// Checker is implemented by a model that declares table-level CHECK
// constraints. Unlike Indexer, an unnamed CheckDef's auto-generated name
// (ck_<table>_<n>) is POSITIONAL: it's the check's 1-based ordinal among
// only the unnamed entries returned by Checks(), in the order returned.
// Reordering Checks() therefore changes an unnamed check's name on the
// next makemigrations run, unlike ix_/uq_ names, which are derived from
// the column set and so are stable under reordering. Name checks
// explicitly with Named if that matters to you:
//
//	func (m *AccountModel) Checks() []orm.CheckDef {
//	    return []orm.CheckDef{
//	        orm.NewCheckDef("balance >= 0").Named("ck_accounts_balance_non_negative"),
//	    }
//	}
type Checker interface {
	Checks() []CheckDef
}
