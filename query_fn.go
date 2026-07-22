package orm

import (
	"fmt"
	"reflect"
)

// The functions below are the ones every database Tork targets spells
// identically. Anything else is written with Fn, where the spelling a caller
// picked is visible at the call site rather than hidden behind a name that
// could not mean the same thing on the next driver: STRING_AGG is
// GROUP_CONCAT in MySQL, DATE_TRUNC has no MySQL equivalent at all, and a
// wrapper promising portability it cannot deliver would be worse than none.
//
// Two shapes appear here, and which one a function takes follows from its
// result. Where the result's type is the function's own — LOWER is text
// whatever it reads — the argument is an any, so a column, an expression or
// a value all pass. Where the result is the argument's type — ABS of a
// decimal is a decimal — the argument is an Expr[T], so T is inferred from
// it; a column reaches that through Value, the same lift its comparisons
// already use:
//
//	orm.Lower(Users.Name)               // a column directly
//	orm.Abs(Items.Delta.Value())        // lifted, so T comes from the column

// Lower is LOWER(v), the text lowercased.
//
//	orm.SelectAs[Row](Users.With(db), orm.Lower(Users.Name))
//	// SELECT LOWER("name") FROM "users"
//
// Matching case-insensitively is better asked with a string column's own
// Contains, StartsWith or EqualsFold, which the dialect renders as ILIKE
// where it has one; this is for reading a value back folded, or for grouping
// and ordering by one.
func Lower(v any) Expr[string] { return textFn("LOWER", v) }

// Upper is UPPER(v), the text uppercased.
func Upper(v any) Expr[string] { return textFn("UPPER", v) }

// Trim is TRIM(v), the text without leading or trailing spaces.
func Trim(v any) Expr[string] { return textFn("TRIM", v) }

// Length is LENGTH(v), how many characters the text holds.
//
// It counts characters rather than bytes in every dialect Tork targets, so a
// name written in any alphabet measures what a reader would count.
func Length(v any) Expr[int64] {
	e := Fn[int64]("LENGTH", v)
	e.n.args[0] = textArg{v}
	return e
}

// textFn builds a call over one text argument, which is checked to be text
// when the statement compiles.
func textFn(name string, v any) Expr[string] {
	e := Fn[string](name, v)
	e.n.args[0] = textArg{v}
	return e
}

// textArg marks an argument a named helper requires to be text, so the
// compiler can say which function was given what rather than leave a column
// of the wrong type to the database. It wraps the argument rather than
// replacing it: what is rendered is still whatever the caller passed.
//
// Only the helpers use it. Fn checks nothing about its arguments, since a
// caller naming a function this package has never heard of is the one who
// knows what it takes.
type textArg struct{ v any }

// Abs is ABS(v), the magnitude of a number without its sign.
//
//	orm.Abs(Items.Delta.Value())  // ABS("delta")
func Abs[T any](v Expr[T]) Expr[T] { return Fn[T]("ABS", v) }

// Round is ROUND(v, places), a number rounded to that many decimal places.
//
//	orm.Round(Items.Price.Value(), 2)  // ROUND("price", CAST($1 AS INTEGER))
func Round[T any](v Expr[T], places int) Expr[T] { return Fn[T]("ROUND", v, places) }

// Coalesce is COALESCE(args...), the first argument that is not NULL.
//
//	orm.Coalesce[string](Users.Nickname, Users.Name)
//	// COALESCE("nickname", "name")
//
// It is what reads a nullable column as a value with a fallback, so a
// projection need not carry a pointer for a column that has a sensible one.
//
// T is written out rather than inferred, unlike Abs and Round, because that
// is the whole point of the call: the arguments are usually nullable and the
// result usually is not, and Go has no way to say "the same type without its
// pointer". Coalescing a *string and a string yields a string, and saying so
// is what lets it be read into an ordinary field.
func Coalesce[T any](args ...any) Expr[T] { return Fn[T]("COALESCE", args...) }

// requireText reports whether an argument is text, for the helpers that
// only make sense over it.
//
// An argument whose type cannot be read is accepted: a nil value says
// nothing about what it was meant to be, and refusing it here would be
// guessing. A nullable column is unwrapped first, since *string is a string
// that may be absent rather than a different kind of value.
func requireText(v any) error {
	t, ok := argType(v)
	if !ok {
		return nil
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.String {
		return fmt.Errorf("this function reads text, but the argument given is %s", t)
	}
	return nil
}

// argType is the Go type an argument is known to carry, and whether it is
// known at all.
//
// A column and a lifted expression both report one. A value reports its own.
// A nil value reports none — it says nothing about what it was meant to be —
// and an unknown type is checked against nothing rather than guessed at.
//
// It takes the argument itself, already unwrapped: callArg strips the
// textArg marker before asking, so what arrives here is only ever the three
// things an argument can be.
func argType(v any) (reflect.Type, bool) {
	switch x := v.(type) {
	case expression:
		return x.GoType(), true
	case ColumnMeta:
		return x.GoType(), true
	case nil:
		return nil, false
	}
	return reflect.TypeOf(v), true
}
